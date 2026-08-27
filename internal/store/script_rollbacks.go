package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

// RollbackScriptVersion republishes a historical community Yarn source as a
// newly compiled immutable version. Historical rows are never reopened or
// promoted back from superseded to published.
func (s *Store) RollbackScriptVersion(
	ctx context.Context,
	accountID, scriptID int64,
	targetVersionNumber int,
) (ScriptVersion, error) {
	var accountRole string
	if err := s.db.QueryRowContext(ctx, `SELECT role FROM accounts WHERE id=$1`, accountID).Scan(&accountRole); errors.Is(err, sql.ErrNoRows) {
		return ScriptVersion{}, ErrNotFound
	} else if err != nil {
		return ScriptVersion{}, err
	}
	if accountRole != "moderator" && accountRole != "admin" {
		return ScriptVersion{}, ErrForbidden
	}

	var origin string
	var archivedAt sql.NullTime
	var currentPublished sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT origin,archived_at,current_published_version_id
		FROM scripts WHERE id=$1`, scriptID).Scan(
		&origin, &archivedAt, &currentPublished,
	); errors.Is(err, sql.ErrNoRows) {
		return ScriptVersion{}, ErrNotFound
	} else if err != nil {
		return ScriptVersion{}, err
	}
	if origin != "community" || archivedAt.Valid {
		return ScriptVersion{}, ErrForbidden
	}

	target, err := scanScriptVersion(s.db.QueryRowContext(ctx, `
		SELECT `+scriptVersionColumns+`
		FROM script_versions v
		WHERE v.script_id=$1 AND v.version=$2 AND v.status='superseded'
	`, scriptID, targetVersionNumber))
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptVersion{}, ErrNotFound
	}
	if err != nil {
		return ScriptVersion{}, err
	}
	if target.ContentFormat != scriptcontent.YarnContentFormat ||
		strings.TrimSpace(target.SourceText) == "" ||
		!currentPublished.Valid || currentPublished.Int64 == target.ID {
		return ScriptVersion{}, errors.New("only a superseded community Yarn version can be rolled back")
	}
	targetAnalysis, err := s.loadScriptAnalysis(ctx, target.ID)
	if err != nil {
		return ScriptVersion{}, err
	}
	compilation, err := s.compileYarn(
		ctx,
		fmt.Sprintf("script-%d-rollback-v%d.yarn", scriptID, targetVersionNumber),
		target.SourceText,
		targetAnalysis.Triggers,
	)
	if err != nil {
		return ScriptVersion{}, err
	}
	if !compilation.Valid || len(compilation.Program) == 0 {
		return ScriptVersion{}, errors.New("historical source does not compile with the current command schema")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptVersion{}, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT role FROM accounts WHERE id=$1 FOR SHARE`, accountID).Scan(&accountRole); err != nil {
		return ScriptVersion{}, ErrNotFound
	}
	if accountRole != "moderator" && accountRole != "admin" {
		return ScriptVersion{}, ErrForbidden
	}
	var lockedOrigin string
	var lockedArchivedAt sql.NullTime
	var lockedCurrentPublished sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT origin,archived_at,current_published_version_id
		FROM scripts WHERE id=$1 FOR UPDATE`, scriptID).Scan(
		&lockedOrigin, &lockedArchivedAt, &lockedCurrentPublished,
	); err != nil {
		return ScriptVersion{}, ErrNotFound
	}
	if lockedOrigin != "community" || lockedArchivedAt.Valid ||
		!lockedCurrentPublished.Valid || lockedCurrentPublished.Int64 != currentPublished.Int64 {
		return ScriptVersion{}, ErrRevisionConflict
	}
	var lockedTargetStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT status FROM script_versions
		WHERE id=$1 AND script_id=$2 FOR SHARE`, target.ID, scriptID,
	).Scan(&lockedTargetStatus); err != nil || lockedTargetStatus != "superseded" {
		return ScriptVersion{}, ErrRevisionConflict
	}

	var nextVersion int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version),0)+1 FROM script_versions WHERE script_id=$1
	`, scriptID).Scan(&nextVersion); err != nil {
		return ScriptVersion{}, err
	}
	summary := rollbackVersionSummary(targetVersionNumber, target.Summary)
	diagnostics := marshalCompilerDiagnostics(compilation.Diagnostics)
	compilerLines, compilerNodes := marshalCompilerMetadata(compilation)
	var versionID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO script_versions (
			script_id,version,content_format,schema_version,document,source_text,
			source_text_hash,compiled_program,compiled_program_hash,compiler_version,
			command_schema_version,compile_status,compiler_diagnostics,compiler_lines,
			compiler_nodes,summary,source_hash,native_source_locator,
			native_source_hash,based_on_version_id,authored_by
		) VALUES (
			$1,$2,'yarn',$3,NULL,$4,$5,$6,$7,$8,$9,'valid',$10,$11,$12,$13,
			NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),$17,$18
		) RETURNING id
	`, scriptID, nextVersion, scriptcontent.YarnContentSchema,
		target.SourceText, scriptcontent.SourceHash(target.SourceText),
		compilation.Program, scriptcontent.BytesHash(compilation.Program),
		scriptcontent.YarnCompilerVersion, scriptcontent.YarnCommandSchemaVersion,
		diagnostics, compilerLines, compilerNodes, summary, target.SourceHash,
		target.NativeSourceLocator, target.NativeSourceHash, target.ID, accountID,
	).Scan(&versionID); err != nil {
		return ScriptVersion{}, fmt.Errorf("create rollback script version: %w", err)
	}
	if err := replaceScriptIndexes(ctx, tx, versionID, compilation.Analysis); err != nil {
		return ScriptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO script_version_native_sources (
			version_id,ordinal,role,source_locator,source_hash
		)
		SELECT $2,ordinal,role,source_locator,source_hash
		FROM script_version_native_sources WHERE version_id=$1
	`, target.ID, versionID); err != nil {
		return ScriptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO script_version_native_dialogue_regions (
			version_id,ordinal,disc,area,executable_target_index,
			region_start_file_offset,ownership,activity_id,evidence_locator
		)
		SELECT $2,ordinal,disc,area,executable_target_index,
		       region_start_file_offset,ownership,activity_id,evidence_locator
		FROM script_version_native_dialogue_regions WHERE version_id=$1
	`, target.ID, versionID); err != nil {
		return ScriptVersion{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE script_versions SET status='review',updated_at=now()
		WHERE id=$1 AND status='draft'
	`, versionID)
	if err != nil {
		return ScriptVersion{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ScriptVersion{}, ErrRevisionConflict
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE script_versions SET status='superseded',updated_at=now()
		WHERE id=$1 AND status='published'
	`, lockedCurrentPublished.Int64)
	if err != nil {
		return ScriptVersion{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ScriptVersion{}, ErrRevisionConflict
	}
	row := tx.QueryRowContext(ctx, `
		UPDATE script_versions AS v
		SET status='published',published_at=now(),updated_at=now()
		WHERE id=$1 AND status='review'
		RETURNING `+scriptVersionColumns, versionID)
	rolledBack, err := scanScriptVersion(row)
	if err != nil {
		return ScriptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE scripts
		SET current_published_version_id=$2,updated_at=now()
		WHERE id=$1
	`, scriptID, versionID); err != nil {
		return ScriptVersion{}, err
	}
	if err := insertScriptModerationEvent(
		ctx, tx, scriptID, &versionID, accountID, "version.rollback-published",
		map[string]any{
			"sourceVersionId": target.ID,
			"sourceVersion":   target.Version,
		},
	); err != nil {
		return ScriptVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptVersion{}, err
	}
	rolledBack.Analysis = compilation.Analysis
	rolledBack.NativeSources, err = s.loadVersionNativeSources(ctx, rolledBack.ID)
	if err != nil {
		return ScriptVersion{}, err
	}
	rolledBack.NativeDialogueRegions, err = s.loadVersionNativeDialogueRegions(ctx, rolledBack.ID)
	if err != nil {
		return ScriptVersion{}, err
	}
	return rolledBack, nil
}

func rollbackVersionSummary(targetVersion int, previous string) string {
	value := fmt.Sprintf("Rollback to version %d", targetVersion)
	if previous = strings.TrimSpace(previous); previous != "" {
		value += ": " + previous
	}
	runes := []rune(value)
	if len(runes) > 4000 {
		value = string(runes[:4000])
	}
	return value
}
