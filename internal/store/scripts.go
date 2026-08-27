package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

var (
	scriptSHA256Pattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	nativeSourceRolePattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	nativeDialogueAreaPattern = regexp.MustCompile(`^[A-Z0-9]{4}$`)
)

type ScriptVersion struct {
	ID                    int64                           `json:"id"`
	ScriptID              int64                           `json:"scriptId"`
	Version               int                             `json:"version"`
	Revision              uint64                          `json:"revision"`
	Status                string                          `json:"status"`
	ContentFormat         string                          `json:"contentFormat"`
	SchemaVersion         string                          `json:"schemaVersion"`
	Document              *scriptcontent.Document         `json:"document,omitempty"`
	SourceText            string                          `json:"sourceText,omitempty"`
	SourceTextHash        string                          `json:"sourceTextHash,omitempty"`
	CompiledProgram       []byte                          `json:"-"`
	CompiledProgramHash   string                          `json:"compiledProgramHash,omitempty"`
	CompilerVersion       string                          `json:"compilerVersion,omitempty"`
	CommandSchemaVersion  string                          `json:"commandSchemaVersion,omitempty"`
	CompileStatus         string                          `json:"compileStatus"`
	CompilerDiagnostics   []scriptcontent.Diagnostic      `json:"compilerDiagnostics"`
	CompilerLines         []scriptcontent.CompiledLine    `json:"compilerLines,omitempty"`
	CompilerNodes         []scriptcontent.CompiledNode    `json:"compilerNodes,omitempty"`
	Summary               string                          `json:"summary"`
	SourceHash            string                          `json:"sourceHash,omitempty"`
	NativeSourceLocator   string                          `json:"nativeSourceLocator,omitempty"`
	NativeSourceHash      string                          `json:"nativeSourceHash,omitempty"`
	NativeSources         []NativeSourceReference         `json:"nativeSources,omitempty"`
	NativeDialogueRegions []NativeDialogueRegionReference `json:"nativeDialogueRegions,omitempty"`
	BasedOnVersion        *int64                          `json:"basedOnVersionId,omitempty"`
	AuthoredBy            *int64                          `json:"authoredBy,omitempty"`
	CreatedAt             time.Time                       `json:"createdAt"`
	UpdatedAt             time.Time                       `json:"updatedAt"`
	PublishedAt           *time.Time                      `json:"publishedAt,omitempty"`
	Analysis              scriptcontent.Analysis          `json:"analysis"`
}

// NativeSourceReference records one immutable recovered input used to author a
// script version. Ordinal preserves reviewed source ordering; Role explains
// what that source proves without overloading its locator.
type NativeSourceReference struct {
	Ordinal int    `json:"ordinal"`
	Role    string `json:"role"`
	Locator string `json:"locator"`
	Hash    string `json:"sha256"`
}

// NativeDialogueRegionReference is an exact reviewed ownership link between a
// published script version and one recovered executable dialogue region. It is
// provenance, not a trigger inferred from actor, voice, area, or proximity.
type NativeDialogueRegionReference struct {
	Ordinal               int    `json:"ordinal"`
	Disc                  int    `json:"disc"`
	Area                  string `json:"area"`
	ExecutableTargetIndex int    `json:"executableTargetIndex"`
	RegionStartFileOffset int64  `json:"regionStartFileOffset"`
	Ownership             string `json:"ownership"`
	ActivityID            string `json:"activityId,omitempty"`
	EvidenceLocator       string `json:"evidenceLocator"`
}

type ScriptSummary struct {
	ID             int64                `json:"id"`
	Slug           string               `json:"slug"`
	Title          string               `json:"title"`
	Description    string               `json:"description"`
	Origin         string               `json:"origin"`
	SourceLocator  string               `json:"sourceLocator,omitempty"`
	SourceHash     string               `json:"sourceHash,omitempty"`
	CreatedBy      *int64               `json:"createdBy,omitempty"`
	AccessRole     string               `json:"accessRole,omitempty"`
	CurrentVersion *ScriptVersionHeader `json:"currentVersion,omitempty"`
	CreatedAt      time.Time            `json:"createdAt"`
	UpdatedAt      time.Time            `json:"updatedAt"`
	ArchivedAt     *time.Time           `json:"archivedAt,omitempty"`
	ArchivedBy     *int64               `json:"archivedBy,omitempty"`
}

type ScriptVersionHeader struct {
	ID            int64     `json:"id"`
	Version       int       `json:"version"`
	Revision      uint64    `json:"revision"`
	Status        string    `json:"status"`
	ContentFormat string    `json:"contentFormat"`
	CompileStatus string    `json:"compileStatus"`
	Summary       string    `json:"summary"`
	NodeCount     int       `json:"nodeCount,omitempty"`
	EdgeCount     int       `json:"edgeCount,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type ScriptDetail struct {
	ScriptSummary
	Versions []ScriptVersion `json:"versions"`
}

type RecoveredScriptImport struct {
	Slug          string                 `json:"slug"`
	Title         string                 `json:"title"`
	Description   string                 `json:"description"`
	SourceLocator string                 `json:"sourceLocator"`
	SourceHash    string                 `json:"sourceHash"`
	DocumentHash  string                 `json:"documentHash"`
	Document      scriptcontent.Document `json:"document"`
	Summary       string                 `json:"summary"`
}

type YarnScriptCreateInput struct {
	Slug        string
	Title       string
	Description string
	Summary     string
	SourceText  string
	Triggers    []scriptcontent.Trigger
}

type YarnDraftUpdateInput struct {
	Revision   uint64
	Summary    string
	SourceText string
	Triggers   []scriptcontent.Trigger
}

type ScriptMetadataUpdate struct {
	Title       string
	Description string
}

func (s *Store) UpdateScriptMetadata(ctx context.Context, accountID, scriptID int64, input ScriptMetadataUpdate) (ScriptDetail, error) {
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	result, err := s.db.ExecContext(ctx, `
		UPDATE scripts s SET title=$3,description=$4,updated_at=now()
		WHERE s.id=$2 AND s.archived_at IS NULL AND (
			EXISTS (SELECT 1 FROM script_collaborators c
				WHERE c.script_id=s.id AND c.account_id=$1
				AND c.role IN ('owner','editor'))
			OR EXISTS (SELECT 1 FROM accounts a WHERE a.id=$1
				AND a.role IN ('moderator','admin'))
		)`, accountID, scriptID, title, description)
	if err != nil {
		return ScriptDetail{}, fmt.Errorf("update script metadata: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return ScriptDetail{}, err
	}
	if updated != 1 {
		return ScriptDetail{}, ErrForbidden
	}
	return s.Script(ctx, accountID, scriptID)
}

func (s *Store) SetScriptArchived(ctx context.Context, accountID, scriptID int64, archived bool) (ScriptDetail, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptDetail{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE scripts s SET
			archived_at=CASE WHEN $3 THEN now() ELSE NULL END,
			archived_by=CASE WHEN $3 THEN $1 ELSE NULL END,
			updated_at=now()
		WHERE s.id=$2 AND s.origin='community' AND (
			EXISTS (SELECT 1 FROM script_collaborators c
				WHERE c.script_id=s.id AND c.account_id=$1 AND c.role='owner')
			OR EXISTS (SELECT 1 FROM accounts a WHERE a.id=$1
				AND a.role IN ('moderator','admin'))
		)`, accountID, scriptID, archived)
	if err != nil {
		return ScriptDetail{}, fmt.Errorf("set script archived: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return ScriptDetail{}, err
	}
	if updated != 1 {
		return ScriptDetail{}, ErrForbidden
	}
	action := "script.restored"
	if archived {
		action = "script.archived"
	}
	if err := insertScriptModerationEvent(ctx, tx, scriptID, nil, accountID, action, nil); err != nil {
		return ScriptDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptDetail{}, err
	}
	return s.Script(ctx, accountID, scriptID)
}

func scanScriptVersion(scanner interface{ Scan(...any) error }) (ScriptVersion, error) {
	var version ScriptVersion
	var document []byte
	var compilerDiagnostics []byte
	var compilerLines, compilerNodes []byte
	var sourceHash, nativeSourceLocator, nativeSourceHash sql.NullString
	var sourceText, sourceTextHash, compiledProgramHash sql.NullString
	var compilerVersion, commandSchemaVersion sql.NullString
	var basedOn sql.NullInt64
	var authoredBy sql.NullInt64
	var publishedAt sql.NullTime
	err := scanner.Scan(
		&version.ID, &version.ScriptID, &version.Version, &version.Revision,
		&version.Status, &version.ContentFormat, &version.SchemaVersion, &document,
		&sourceText, &sourceTextHash, &version.CompiledProgram,
		&compiledProgramHash, &compilerVersion, &commandSchemaVersion,
		&version.CompileStatus, &compilerDiagnostics, &version.Summary, &sourceHash,
		&nativeSourceLocator, &nativeSourceHash,
		&compilerLines, &compilerNodes,
		&basedOn, &authoredBy, &version.CreatedAt, &version.UpdatedAt, &publishedAt,
	)
	if err != nil {
		return ScriptVersion{}, err
	}
	if len(document) > 0 {
		var decoded scriptcontent.Document
		if err := json.Unmarshal(document, &decoded); err != nil {
			return ScriptVersion{}, fmt.Errorf("decode script version: %w", err)
		}
		version.Document = &decoded
	}
	version.SourceText = sourceText.String
	version.SourceTextHash = sourceTextHash.String
	version.CompiledProgramHash = compiledProgramHash.String
	version.CompilerVersion = compilerVersion.String
	version.CommandSchemaVersion = commandSchemaVersion.String
	if err := json.Unmarshal(compilerDiagnostics, &version.CompilerDiagnostics); err != nil {
		return ScriptVersion{}, fmt.Errorf("decode script diagnostics: %w", err)
	}
	if len(compilerLines) > 0 {
		if err := json.Unmarshal(compilerLines, &version.CompilerLines); err != nil {
			return ScriptVersion{}, fmt.Errorf("decode compiled Yarn lines: %w", err)
		}
	}
	if len(compilerNodes) > 0 {
		if err := json.Unmarshal(compilerNodes, &version.CompilerNodes); err != nil {
			return ScriptVersion{}, fmt.Errorf("decode compiled Yarn nodes: %w", err)
		}
	}
	version.SourceHash = sourceHash.String
	version.NativeSourceLocator = nativeSourceLocator.String
	version.NativeSourceHash = nativeSourceHash.String
	if basedOn.Valid {
		value := basedOn.Int64
		version.BasedOnVersion = &value
	}
	if authoredBy.Valid {
		value := authoredBy.Int64
		version.AuthoredBy = &value
	}
	if publishedAt.Valid {
		value := publishedAt.Time
		version.PublishedAt = &value
	}
	if version.Document != nil {
		analysis, err := scriptcontent.Validate(*version.Document, false)
		if err != nil {
			return ScriptVersion{}, fmt.Errorf("validate stored script version: %w", err)
		}
		version.Analysis = analysis
	}
	return version, nil
}

const scriptVersionColumns = `
	v.id, v.script_id, v.version, v.revision, v.status, v.content_format,
	v.schema_version, v.document, v.source_text, v.source_text_hash,
	v.compiled_program, v.compiled_program_hash, v.compiler_version,
	v.command_schema_version, v.compile_status, v.compiler_diagnostics,
	v.summary, v.source_hash, v.native_source_locator, v.native_source_hash,
	v.compiler_lines, v.compiler_nodes,
	v.based_on_version_id, v.authored_by,
	v.created_at, v.updated_at, v.published_at`

func (s *Store) ListScripts(ctx context.Context, accountID int64) ([]ScriptSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.slug, s.title, s.description, s.origin,
		       s.source_locator, s.source_hash, s.created_by,
		       COALESCE(c.role, CASE WHEN viewer.role IN ('moderator','admin') THEN 'reviewer' ELSE '' END),
		       s.created_at, s.updated_at, s.archived_at, s.archived_by,
		       v.id, v.version, v.revision, v.status, v.content_format,
		       v.compile_status, v.summary,
		       COALESCE(jsonb_array_length(v.document->'nodes'), 0),
		       COALESCE(jsonb_array_length(v.document->'edges'), 0), v.updated_at
		FROM scripts s
		LEFT JOIN script_collaborators c
		  ON c.script_id = s.id AND c.account_id = NULLIF($1, 0)
		LEFT JOIN accounts viewer ON viewer.id = NULLIF($1, 0)
		JOIN LATERAL (
			SELECT candidate.* FROM script_versions candidate
			WHERE candidate.script_id = s.id
			  AND (candidate.status IN ('published', 'reference') OR c.account_id IS NOT NULL OR viewer.role IN ('moderator','admin'))
			ORDER BY
			  CASE WHEN c.account_id IS NOT NULL OR viewer.role IN ('moderator','admin') THEN candidate.version ELSE 0 END DESC,
			  CASE WHEN candidate.id IN (s.current_published_version_id, s.current_reference_version_id) THEN 1 ELSE 0 END DESC,
			  candidate.version DESC
			LIMIT 1
		) v ON true
		WHERE s.archived_at IS NULL OR c.account_id IS NOT NULL
		   OR viewer.role IN ('moderator','admin')
		ORDER BY s.updated_at DESC, s.id DESC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("list scripts: %w", err)
	}
	defer rows.Close()
	result := []ScriptSummary{}
	for rows.Next() {
		var script ScriptSummary
		var sourceLocator, sourceHash sql.NullString
		var createdBy sql.NullInt64
		var archivedAt sql.NullTime
		var archivedBy sql.NullInt64
		var version ScriptVersionHeader
		if err := rows.Scan(
			&script.ID, &script.Slug, &script.Title, &script.Description,
			&script.Origin, &sourceLocator, &sourceHash, &createdBy,
			&script.AccessRole, &script.CreatedAt, &script.UpdatedAt,
			&archivedAt, &archivedBy,
			&version.ID, &version.Version, &version.Revision, &version.Status,
			&version.ContentFormat, &version.CompileStatus, &version.Summary,
			&version.NodeCount, &version.EdgeCount,
			&version.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan script list: %w", err)
		}
		script.SourceLocator, script.SourceHash = sourceLocator.String, sourceHash.String
		if createdBy.Valid {
			value := createdBy.Int64
			script.CreatedBy = &value
		}
		if archivedAt.Valid {
			value := archivedAt.Time
			script.ArchivedAt = &value
		}
		if archivedBy.Valid {
			value := archivedBy.Int64
			script.ArchivedBy = &value
		}
		script.CurrentVersion = &version
		result = append(result, script)
	}
	return result, rows.Err()
}

func (s *Store) Script(ctx context.Context, accountID, scriptID int64) (ScriptDetail, error) {
	var detail ScriptDetail
	var sourceLocator, sourceHash sql.NullString
	var createdBy sql.NullInt64
	var archivedAt sql.NullTime
	var archivedBy sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.slug, s.title, s.description, s.origin,
		       s.source_locator, s.source_hash, s.created_by,
		       COALESCE(c.role, CASE WHEN viewer.role IN ('moderator','admin') THEN 'reviewer' ELSE '' END),
		       s.created_at, s.updated_at, s.archived_at, s.archived_by
		FROM scripts s
		LEFT JOIN script_collaborators c
		  ON c.script_id = s.id AND c.account_id = NULLIF($1, 0)
		LEFT JOIN accounts viewer ON viewer.id = NULLIF($1, 0)
		WHERE s.id = $2 AND (
		  c.account_id IS NOT NULL OR s.current_published_version_id IS NOT NULL
		  OR s.current_reference_version_id IS NOT NULL OR viewer.role IN ('moderator','admin')
		) AND (
		  s.archived_at IS NULL OR c.account_id IS NOT NULL
		  OR viewer.role IN ('moderator','admin')
		)`, accountID, scriptID).Scan(
		&detail.ID, &detail.Slug, &detail.Title, &detail.Description,
		&detail.Origin, &sourceLocator, &sourceHash, &createdBy,
		&detail.AccessRole, &detail.CreatedAt, &detail.UpdatedAt,
		&archivedAt, &archivedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptDetail{}, ErrNotFound
	}
	if err != nil {
		return ScriptDetail{}, fmt.Errorf("load script: %w", err)
	}
	detail.SourceLocator, detail.SourceHash = sourceLocator.String, sourceHash.String
	if createdBy.Valid {
		value := createdBy.Int64
		detail.CreatedBy = &value
	}
	if archivedAt.Valid {
		value := archivedAt.Time
		detail.ArchivedAt = &value
	}
	if archivedBy.Valid {
		value := archivedBy.Int64
		detail.ArchivedBy = &value
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+scriptVersionColumns+`
		FROM script_versions v
		WHERE v.script_id = $1
		  AND (v.status IN ('published', 'reference', 'superseded') OR $2 <> '')
		ORDER BY v.version DESC`, scriptID, detail.AccessRole)
	if err != nil {
		return ScriptDetail{}, fmt.Errorf("load script versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		version, err := scanScriptVersion(rows)
		if err != nil {
			return ScriptDetail{}, err
		}
		detail.Versions = append(detail.Versions, version)
	}
	if err := rows.Err(); err != nil {
		return ScriptDetail{}, err
	}
	if err := rows.Close(); err != nil {
		return ScriptDetail{}, err
	}
	for index := range detail.Versions {
		nativeSources, err := s.loadVersionNativeSources(ctx, detail.Versions[index].ID)
		if err != nil {
			return ScriptDetail{}, err
		}
		detail.Versions[index].NativeSources = nativeSources
		nativeDialogueRegions, err := s.loadVersionNativeDialogueRegions(ctx, detail.Versions[index].ID)
		if err != nil {
			return ScriptDetail{}, err
		}
		detail.Versions[index].NativeDialogueRegions = nativeDialogueRegions
		if detail.Versions[index].ContentFormat != scriptcontent.YarnContentFormat {
			continue
		}
		analysis, err := s.loadScriptAnalysis(ctx, detail.Versions[index].ID)
		if err != nil {
			return ScriptDetail{}, err
		}
		detail.Versions[index].Analysis = analysis
	}
	if len(detail.Versions) > 0 {
		version := detail.Versions[0]
		nodeCount, edgeCount := 0, 0
		if version.Document != nil {
			nodeCount, edgeCount = len(version.Document.Nodes), len(version.Document.Edges)
		}
		detail.CurrentVersion = &ScriptVersionHeader{
			ID: version.ID, Version: version.Version, Revision: version.Revision,
			Status: version.Status, ContentFormat: version.ContentFormat,
			CompileStatus: version.CompileStatus, Summary: version.Summary,
			NodeCount: nodeCount, EdgeCount: edgeCount,
			UpdatedAt: version.UpdatedAt,
		}
	}
	return detail, nil
}

func (s *Store) loadVersionNativeSources(ctx context.Context, versionID int64) ([]NativeSourceReference, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ordinal,role,source_locator,source_hash
		FROM script_version_native_sources WHERE version_id=$1 ORDER BY ordinal`, versionID)
	if err != nil {
		return nil, fmt.Errorf("load script native sources: %w", err)
	}
	defer rows.Close()
	sources := []NativeSourceReference{}
	for rows.Next() {
		var source NativeSourceReference
		if err := rows.Scan(&source.Ordinal, &source.Role, &source.Locator, &source.Hash); err != nil {
			return nil, fmt.Errorf("scan script native source: %w", err)
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (s *Store) loadVersionNativeDialogueRegions(ctx context.Context, versionID int64) ([]NativeDialogueRegionReference, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ordinal,disc,area,executable_target_index,
		region_start_file_offset,ownership,COALESCE(activity_id,''),evidence_locator
		FROM script_version_native_dialogue_regions WHERE version_id=$1 ORDER BY ordinal`, versionID)
	if err != nil {
		return nil, fmt.Errorf("load script native dialogue regions: %w", err)
	}
	defer rows.Close()
	regions := []NativeDialogueRegionReference{}
	for rows.Next() {
		var region NativeDialogueRegionReference
		if err := rows.Scan(&region.Ordinal, &region.Disc, &region.Area,
			&region.ExecutableTargetIndex, &region.RegionStartFileOffset,
			&region.Ownership, &region.ActivityID, &region.EvidenceLocator); err != nil {
			return nil, fmt.Errorf("scan script native dialogue region: %w", err)
		}
		regions = append(regions, region)
	}
	return regions, rows.Err()
}

func replaceScriptIndexes(ctx context.Context, tx *sql.Tx, versionID int64, analysis scriptcontent.Analysis) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM script_version_dependencies WHERE version_id = $1`, versionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM script_version_triggers WHERE version_id = $1`, versionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM script_version_identifiers WHERE version_id = $1`, versionID); err != nil {
		return err
	}
	for _, dependency := range analysis.Dependencies {
		if _, err := tx.ExecContext(ctx, `INSERT INTO script_version_dependencies (version_id, access, kind, identifier) VALUES ($1,$2,$3,$4)`, versionID, dependency.Access, dependency.Kind, dependency.Identifier); err != nil {
			return err
		}
	}
	for _, identifier := range indexedScriptIdentifiers(analysis) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO script_version_identifiers (version_id,kind,identifier) VALUES ($1,$2,$3)`, versionID, identifier.Kind, identifier.Identifier); err != nil {
			return err
		}
	}
	for _, trigger := range analysis.Triggers {
		configuration, _ := json.Marshal(trigger.Configuration)
		if _, err := tx.ExecContext(ctx, `INSERT INTO script_version_triggers (
			version_id,node_id,kind,area,actor,object_key,activity_key,priority,configuration
		) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9)`,
			versionID, trigger.NodeID, trigger.Kind, trigger.Area, trigger.Actor,
			trigger.Object, trigger.Activity, trigger.Priority, configuration); err != nil {
			return err
		}
	}
	return nil
}

func indexedScriptIdentifiers(analysis scriptcontent.Analysis) []scriptcontent.IdentifierUsage {
	identifiers := make(map[string]scriptcontent.IdentifierUsage)
	for _, identifier := range analysis.Identifiers {
		identifiers[identifier.Kind+"\x00"+identifier.Identifier] = identifier
	}
	for _, dependency := range analysis.Dependencies {
		identifier := scriptcontent.IdentifierUsage{Kind: dependency.Kind, Identifier: dependency.Identifier}
		identifiers[identifier.Kind+"\x00"+identifier.Identifier] = identifier
	}
	for _, trigger := range analysis.Triggers {
		for _, identifier := range []scriptcontent.IdentifierUsage{
			{Kind: "scene", Identifier: trigger.Area},
			{Kind: "actor", Identifier: trigger.Actor},
			{Kind: "object", Identifier: trigger.Object},
			{Kind: "activity", Identifier: trigger.Activity},
		} {
			if identifier.Identifier != "" {
				identifiers[identifier.Kind+"\x00"+identifier.Identifier] = identifier
			}
		}
	}
	result := make([]scriptcontent.IdentifierUsage, 0, len(identifiers))
	for _, identifier := range identifiers {
		result = append(result, identifier)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		return result[left].Identifier < result[right].Identifier
	})
	return result
}

func (s *Store) CreateYarnScript(ctx context.Context, accountID int64, input YarnScriptCreateInput) (ScriptDetail, error) {
	canonicalSource, err := scriptcontent.CanonicalYarnSource(input.SourceText)
	if err != nil {
		return ScriptDetail{}, err
	}
	slug := strings.ToLower(strings.TrimSpace(input.Slug))
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	summary := strings.TrimSpace(input.Summary)
	compilation, err := s.compileYarn(ctx, slug+".yarn", canonicalSource, input.Triggers)
	if err != nil {
		return ScriptDetail{}, err
	}
	compileStatus := "invalid"
	var compiledHash any
	if compilation.Valid {
		compileStatus = "valid"
		compiledHash = scriptcontent.BytesHash(compilation.Program)
	}
	diagnostics := marshalCompilerDiagnostics(compilation.Diagnostics)
	compilerLines, compilerNodes := marshalCompilerMetadata(compilation)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptDetail{}, err
	}
	defer tx.Rollback()
	var scriptID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO scripts (slug,title,description,origin,created_by) VALUES ($1,$2,$3,'community',$4) RETURNING id`, slug, title, description, accountID).Scan(&scriptID); err != nil {
		return ScriptDetail{}, fmt.Errorf("create Yarn script: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO script_collaborators (script_id,account_id,role) VALUES ($1,$2,'owner')`, scriptID, accountID); err != nil {
		return ScriptDetail{}, err
	}
	var versionID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO script_versions (
			script_id,version,content_format,schema_version,document,source_text,
			source_text_hash,compiled_program,compiled_program_hash,compiler_version,
			command_schema_version,compile_status,compiler_diagnostics,compiler_lines,
			compiler_nodes,summary,authored_by
		) VALUES ($1,1,'yarn',$2,NULL,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id`,
		scriptID, scriptcontent.YarnContentSchema, canonicalSource,
		scriptcontent.SourceHash(canonicalSource), compilation.Program, compiledHash,
		scriptcontent.YarnCompilerVersion, scriptcontent.YarnCommandSchemaVersion,
		compileStatus, diagnostics, compilerLines, compilerNodes,
		summary, accountID).Scan(&versionID); err != nil {
		return ScriptDetail{}, err
	}
	if err := replaceScriptIndexes(ctx, tx, versionID, compilation.Analysis); err != nil {
		return ScriptDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptDetail{}, err
	}
	return s.Script(ctx, accountID, scriptID)
}

func (s *Store) SaveYarnScriptDraft(ctx context.Context, accountID, scriptID int64, versionNumber int, input YarnDraftUpdateInput) (ScriptVersion, error) {
	canonicalSource, err := scriptcontent.CanonicalYarnSource(input.SourceText)
	if err != nil {
		return ScriptVersion{}, err
	}
	compilation, err := s.compileYarn(ctx, fmt.Sprintf("script-%d.yarn", scriptID), canonicalSource, input.Triggers)
	if err != nil {
		return ScriptVersion{}, err
	}
	compileStatus := "invalid"
	var compiledHash any
	if compilation.Valid {
		compileStatus = "valid"
		compiledHash = scriptcontent.BytesHash(compilation.Program)
	}
	diagnostics := marshalCompilerDiagnostics(compilation.Diagnostics)
	compilerLines, compilerNodes := marshalCompilerMetadata(compilation)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptVersion{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `UPDATE script_versions v SET
			source_text=$5, source_text_hash=$6, compiled_program=$7,
			compiled_program_hash=$8, compiler_version=$9, command_schema_version=$10,
			compile_status=$11, compiler_diagnostics=$12, compiler_lines=$13,
			compiler_nodes=$14, summary=$15,
			revision=revision+1, updated_at=now()
		FROM script_collaborators c
		WHERE v.script_id=$1 AND v.version=$2 AND v.revision=$3
		  AND v.status='draft' AND v.content_format='yarn'
		  AND c.script_id=v.script_id AND c.account_id=$4
		  AND c.role IN ('owner','editor')
		RETURNING `+scriptVersionColumns,
		scriptID, versionNumber, input.Revision, accountID, canonicalSource,
		scriptcontent.SourceHash(canonicalSource), compilation.Program, compiledHash,
		scriptcontent.YarnCompilerVersion, scriptcontent.YarnCommandSchemaVersion,
		compileStatus, diagnostics, compilerLines, compilerNodes,
		strings.TrimSpace(input.Summary))
	version, err := scanScriptVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptVersion{}, ErrRevisionConflict
	}
	if err != nil {
		return ScriptVersion{}, err
	}
	if err := replaceScriptIndexes(ctx, tx, version.ID, compilation.Analysis); err != nil {
		return ScriptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scripts SET updated_at=now() WHERE id=$1`, scriptID); err != nil {
		return ScriptVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptVersion{}, err
	}
	version.Analysis = compilation.Analysis
	return version, nil
}

func (s *Store) compileYarn(ctx context.Context, fileName, source string, triggers []scriptcontent.Trigger) (scriptcontent.Compilation, error) {
	if s.scriptCompiler == nil {
		return scriptcontent.Compilation{}, errors.New("Yarn compiler is not configured")
	}
	compilation, err := s.scriptCompiler.Compile(ctx, fileName, source)
	if err != nil {
		return scriptcontent.Compilation{}, fmt.Errorf("compile Yarn source: %w", err)
	}
	normalizedTriggers, triggerDiagnostics := scriptcontent.ValidateYarnTriggers(triggers, compilation.Nodes)
	compilation.Analysis.Triggers = normalizedTriggers
	compilation.Diagnostics = append(compilation.Diagnostics, triggerDiagnostics...)
	if len(triggerDiagnostics) > 0 {
		compilation.Valid = false
		compilation.Program = nil
	}
	return compilation, nil
}

func marshalCompilerDiagnostics(diagnostics []scriptcontent.Diagnostic) []byte {
	if diagnostics == nil {
		diagnostics = []scriptcontent.Diagnostic{}
	}
	encoded, _ := json.Marshal(diagnostics)
	return encoded
}

func marshalCompilerMetadata(compilation scriptcontent.Compilation) ([]byte, []byte) {
	lines := compilation.Lines
	if lines == nil {
		lines = []scriptcontent.CompiledLine{}
	}
	nodes := compilation.Nodes
	if nodes == nil {
		nodes = []scriptcontent.CompiledNode{}
	}
	encodedLines, _ := json.Marshal(lines)
	encodedNodes, _ := json.Marshal(nodes)
	return encodedLines, encodedNodes
}

func (s *Store) CreateScriptVersion(ctx context.Context, accountID, scriptID int64, basedOn int) (ScriptVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptVersion{}, err
	}
	defer tx.Rollback()
	var role string
	var currentPublished, currentReference sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(c.role,''),s.current_published_version_id,s.current_reference_version_id
		FROM scripts s
		LEFT JOIN script_collaborators c ON c.script_id=s.id AND c.account_id=$2
		WHERE s.id=$1
		FOR UPDATE OF s`, scriptID, accountID).Scan(&role, &currentPublished, &currentReference)
	if err != nil {
		return ScriptVersion{}, ErrNotFound
	}
	var baseID int64
	var contentFormat string
	if err := tx.QueryRowContext(ctx, `SELECT id,content_format FROM script_versions WHERE script_id=$1 AND version=$2`, scriptID, basedOn).Scan(&baseID, &contentFormat); err != nil {
		return ScriptVersion{}, ErrNotFound
	}
	if contentFormat == "native-reference" {
		return ScriptVersion{}, errors.New("native references must be translated to Yarn before editing")
	}
	if role == "reviewer" {
		return ScriptVersion{}, ErrForbidden
	}
	if role != "owner" && role != "editor" {
		if (!currentPublished.Valid || baseID != currentPublished.Int64) && (!currentReference.Valid || baseID != currentReference.Int64) {
			return ScriptVersion{}, ErrNotFound
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO script_collaborators (script_id,account_id,role) VALUES ($1,$2,'editor') ON CONFLICT (script_id,account_id) DO NOTHING`, scriptID, accountID)
		if err != nil {
			return ScriptVersion{}, err
		}
		if count, _ := result.RowsAffected(); count == 1 {
			if err := insertScriptModerationEvent(
				ctx, tx, scriptID, nil, accountID, "collaborator.added",
				map[string]any{"accountId": accountID, "role": "editor"},
			); err != nil {
				return ScriptVersion{}, err
			}
		}
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM script_versions WHERE script_id=$1`, scriptID).Scan(&next); err != nil {
		return ScriptVersion{}, err
	}
	row := tx.QueryRowContext(ctx, `INSERT INTO script_versions AS v (
			script_id,version,schema_version,document,summary,source_hash,
			native_source_locator,native_source_hash,
			based_on_version_id,authored_by,content_format,source_text,
			source_text_hash,compiled_program,compiled_program_hash,
			compiler_version,command_schema_version,compile_status,
			compiler_diagnostics,compiler_lines,compiler_nodes
		)
		SELECT script_id,$2,schema_version,document,summary,source_hash,
		       native_source_locator,native_source_hash,
		       id,$3,content_format,source_text,source_text_hash,compiled_program,
		       compiled_program_hash,compiler_version,command_schema_version,
		       compile_status,compiler_diagnostics,compiler_lines,compiler_nodes
		FROM script_versions WHERE id=$1
		RETURNING `+scriptVersionColumns, baseID, next, accountID)
	version, err := scanScriptVersion(row)
	if err != nil {
		return ScriptVersion{}, err
	}
	analysis := scriptcontent.Analysis{}
	if version.Document != nil {
		analysis, _ = scriptcontent.Validate(*version.Document, false)
		if err := replaceScriptIndexes(ctx, tx, version.ID, analysis); err != nil {
			return ScriptVersion{}, err
		}
	} else if err := copyScriptIndexes(ctx, tx, baseID, version.ID); err != nil {
		return ScriptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO script_version_native_sources
		(version_id,ordinal,role,source_locator,source_hash)
		SELECT $2,ordinal,role,source_locator,source_hash
		FROM script_version_native_sources WHERE version_id=$1`, baseID, version.ID); err != nil {
		return ScriptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO script_version_native_dialogue_regions
		(version_id,ordinal,disc,area,executable_target_index,region_start_file_offset,
		 ownership,activity_id,evidence_locator)
		SELECT $2,ordinal,disc,area,executable_target_index,region_start_file_offset,
		       ownership,activity_id,evidence_locator
		FROM script_version_native_dialogue_regions WHERE version_id=$1`, baseID, version.ID); err != nil {
		return ScriptVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptVersion{}, err
	}
	if version.ContentFormat == scriptcontent.YarnContentFormat {
		analysis, err := s.loadScriptAnalysis(ctx, version.ID)
		if err != nil {
			return ScriptVersion{}, err
		}
		version.Analysis = analysis
	}
	version.NativeSources, err = s.loadVersionNativeSources(ctx, version.ID)
	if err != nil {
		return ScriptVersion{}, err
	}
	version.NativeDialogueRegions, err = s.loadVersionNativeDialogueRegions(ctx, version.ID)
	if err != nil {
		return ScriptVersion{}, err
	}
	return version, nil
}

func (s *Store) SubmitScriptVersion(ctx context.Context, accountID, scriptID int64, versionNumber int) (ScriptVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptVersion{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `UPDATE script_versions v SET status='review', updated_at=now()
		FROM script_collaborators c WHERE v.script_id=$1 AND v.version=$2 AND v.status='draft'
		AND (v.content_format <> 'yarn' OR (
			v.compile_status='valid' AND v.compiler_version=$4
			AND v.command_schema_version=$5
		))
		AND c.script_id=v.script_id AND c.account_id=$3 AND c.role IN ('owner','editor') RETURNING `+scriptVersionColumns,
		scriptID, versionNumber, accountID,
		scriptcontent.YarnCompilerVersion, scriptcontent.YarnCommandSchemaVersion)
	version, err := scanScriptVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptVersion{}, ErrNotFound
	}
	if err != nil {
		return ScriptVersion{}, err
	}
	if err := insertScriptModerationEvent(ctx, tx, scriptID, &version.ID, accountID, "version.submitted", nil); err != nil {
		return ScriptVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptVersion{}, err
	}
	return version, nil
}

func (s *Store) PublishScriptVersion(ctx context.Context, accountID, scriptID int64, versionNumber int) (ScriptVersion, error) {
	var accountRole string
	if err := s.db.QueryRowContext(ctx, `SELECT role FROM accounts WHERE id=$1`, accountID).Scan(&accountRole); errors.Is(err, sql.ErrNoRows) {
		return ScriptVersion{}, ErrNotFound
	} else if err != nil {
		return ScriptVersion{}, err
	}
	if accountRole != "moderator" && accountRole != "admin" {
		return ScriptVersion{}, ErrForbidden
	}
	target, err := scanScriptVersion(s.db.QueryRowContext(ctx, `SELECT `+scriptVersionColumns+` FROM script_versions v WHERE v.script_id=$1 AND v.version=$2`, scriptID, versionNumber))
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptVersion{}, ErrNotFound
	}
	if err != nil {
		return ScriptVersion{}, err
	}
	if target.ContentFormat == "yarn" {
		if target.CompileStatus != "valid" || len(target.CompiledProgram) == 0 {
			return ScriptVersion{}, errors.New("Yarn script must compile successfully before publication")
		}
		if target.CompilerVersion != scriptcontent.YarnCompilerVersion ||
			target.CommandSchemaVersion != scriptcontent.YarnCommandSchemaVersion {
			return ScriptVersion{}, errors.New("Yarn script must be recompiled with the current compiler and command schema before publication")
		}
	} else {
		if target.Document == nil {
			return ScriptVersion{}, errors.New("graph script document is missing")
		}
		if _, err := scriptcontent.Validate(*target.Document, true); err != nil {
			return ScriptVersion{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptVersion{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE script_versions SET status='superseded' WHERE id=(SELECT current_published_version_id FROM scripts WHERE id=$1) AND status='published'`, scriptID); err != nil {
		return ScriptVersion{}, err
	}
	row := tx.QueryRowContext(ctx, `UPDATE script_versions AS v SET status='published',published_at=now(),updated_at=now() WHERE script_id=$1 AND version=$2 AND status='review' RETURNING `+scriptVersionColumns, scriptID, versionNumber)
	version, err := scanScriptVersion(row)
	if err != nil {
		return ScriptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scripts SET current_published_version_id=$2,updated_at=now() WHERE id=$1`, scriptID, version.ID); err != nil {
		return ScriptVersion{}, err
	}
	if err := insertScriptModerationEvent(ctx, tx, scriptID, &version.ID, accountID, "version.published", nil); err != nil {
		return ScriptVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptVersion{}, err
	}
	return version, nil
}

func (s *Store) ImportRecoveredScript(ctx context.Context, input RecoveredScriptImport) (ScriptDetail, error) {
	analysis, err := scriptcontent.Validate(input.Document, false)
	if err != nil {
		return ScriptDetail{}, err
	}
	if !scriptSHA256Pattern.MatchString(input.SourceHash) || !scriptSHA256Pattern.MatchString(input.DocumentHash) {
		return ScriptDetail{}, errors.New("recovered script hashes must be SHA-256 values")
	}
	documentJSON, _ := json.Marshal(input.Document)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptDetail{}, err
	}
	defer tx.Rollback()
	var scriptID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO scripts (slug,title,description,origin,source_locator,source_hash) VALUES ($1,$2,$3,'recovered',$4,$5)
		ON CONFLICT (source_locator) WHERE origin='recovered' DO UPDATE SET title=EXCLUDED.title,description=EXCLUDED.description,source_hash=EXCLUDED.source_hash,updated_at=now() RETURNING id`, input.Slug, input.Title, input.Description, input.SourceLocator, input.SourceHash).Scan(&scriptID)
	if err != nil {
		return ScriptDetail{}, err
	}
	var existingID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM script_versions WHERE script_id=$1 AND source_hash=$2 LIMIT 1`, scriptID, input.DocumentHash).Scan(&existingID)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return ScriptDetail{}, err
		}
		return s.Script(ctx, 0, scriptID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ScriptDetail{}, err
	}
	var next int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM script_versions WHERE script_id=$1`, scriptID).Scan(&next)
	var versionID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO script_versions (
		script_id,version,status,content_format,schema_version,document,summary,source_hash,
		native_source_locator,native_source_hash
	) VALUES ($1,$2,'draft','native-reference',$3,$4,$5,$6,$7,$8) RETURNING id`,
		scriptID, next, scriptcontent.Schema, documentJSON, input.Summary, input.DocumentHash,
		input.SourceLocator, input.SourceHash).Scan(&versionID); err != nil {
		return ScriptDetail{}, err
	}
	if err := replaceScriptIndexes(ctx, tx, versionID, analysis); err != nil {
		return ScriptDetail{}, err
	}
	if err := insertVersionNativeSources(ctx, tx, versionID, []NativeSourceReference{{
		Ordinal: 0, Role: "primary", Locator: input.SourceLocator, Hash: input.SourceHash,
	}}); err != nil {
		return ScriptDetail{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE script_versions SET status='reference',published_at=now() WHERE id=$1`, versionID); err != nil {
		return ScriptDetail{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scripts SET current_reference_version_id=$2 WHERE id=$1`, scriptID, versionID); err != nil {
		return ScriptDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptDetail{}, err
	}
	return s.Script(ctx, 0, scriptID)
}
