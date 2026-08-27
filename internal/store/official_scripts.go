package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

// OfficialYarnImport is a reviewed translation of recovered native behavior.
// NativeSources identify every immutable recovered input. SourceLocator and
// SourceHash remain the primary-source compatibility fields used by older
// import callers; new multi-source definitions should populate NativeSources.
type OfficialYarnImport struct {
	Slug                  string
	Title                 string
	Description           string
	Summary               string
	SourceLocator         string
	SourceHash            string
	NativeSources         []NativeSourceReference
	NativeDialogueRegions []NativeDialogueRegionReference
	SourceText            string
	Triggers              []scriptcontent.Trigger
	TestFixtures          []OfficialScriptTestFixture
}

// OfficialScriptTestFixture is a reviewed branch setup shipped with an
// official translation. Fixture is JSON so this storage package does not own
// the preview runtime's world-state model.
type OfficialScriptTestFixture struct {
	Name        string
	Description string
	StartNode   string
	Fixture     json.RawMessage
}

// ImportOfficialYarnScript idempotently compiles and publishes trusted official
// content. Runtime execution always reads the immutable database version.
func (s *Store) ImportOfficialYarnScript(ctx context.Context, input OfficialYarnImport) (ScriptDetail, error) {
	canonicalSource, err := scriptcontent.CanonicalYarnSource(input.SourceText)
	if err != nil {
		return ScriptDetail{}, err
	}
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.Summary = strings.TrimSpace(input.Summary)
	nativeSources, err := normalizeOfficialNativeSources(input)
	if err != nil {
		return ScriptDetail{}, err
	}
	nativeDialogueRegions, err := normalizeOfficialNativeDialogueRegions(input.NativeDialogueRegions)
	if err != nil {
		return ScriptDetail{}, err
	}
	input.SourceLocator, input.SourceHash = nativeSources[0].Locator, nativeSources[0].Hash
	compilation, err := s.compileYarn(ctx, input.Slug+".yarn", canonicalSource, input.Triggers)
	if err != nil {
		return ScriptDetail{}, err
	}
	if !compilation.Valid || len(compilation.Program) == 0 {
		return ScriptDetail{}, fmt.Errorf("official Yarn translation is invalid: %s", diagnosticSummary(compilation.Diagnostics))
	}
	testFixtures, err := normalizeOfficialTestFixtures(input.TestFixtures, compilation.Nodes)
	if err != nil {
		return ScriptDetail{}, err
	}
	triggerJSON, err := json.Marshal(compilation.Analysis.Triggers)
	if err != nil {
		return ScriptDetail{}, err
	}
	contentHash := scriptcontent.BytesHash(append(append([]byte(canonicalSource), 0), triggerJSON...))
	diagnostics := marshalCompilerDiagnostics(compilation.Diagnostics)
	compilerLines, compilerNodes := marshalCompilerMetadata(compilation)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptDetail{}, err
	}
	defer tx.Rollback()
	var scriptID int64
	var origin string
	err = tx.QueryRowContext(ctx, `SELECT id,origin FROM scripts WHERE slug=$1 FOR UPDATE`, input.Slug).Scan(&scriptID, &origin)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		err = tx.QueryRowContext(ctx, `INSERT INTO scripts (
			slug,title,description,origin,source_locator,source_hash
		) VALUES ($1,$2,$3,'official',$4,$5) RETURNING id`,
			input.Slug, input.Title, input.Description, input.SourceLocator, input.SourceHash).Scan(&scriptID)
	case err != nil:
		return ScriptDetail{}, err
	case origin != "official":
		return ScriptDetail{}, fmt.Errorf("script slug %q belongs to %s content", input.Slug, origin)
	default:
		_, err = tx.ExecContext(ctx, `UPDATE scripts SET
			title=$2,description=$3,source_locator=$4,source_hash=$5,updated_at=now()
			WHERE id=$1`, scriptID, input.Title, input.Description, input.SourceLocator, input.SourceHash)
	}
	if err != nil {
		return ScriptDetail{}, err
	}

	var currentVersionID int64
	var currentHash, currentCompiler, currentSchema string
	var currentNativeLocator, currentNativeHash string
	err = tx.QueryRowContext(ctx, `SELECT v.id,COALESCE(v.source_hash,''),
		COALESCE(v.compiler_version,''),COALESCE(v.command_schema_version,''),
		COALESCE(v.native_source_locator,''),COALESCE(v.native_source_hash,'')
		FROM scripts script JOIN script_versions v ON v.id=script.current_published_version_id
		WHERE script.id=$1`, scriptID).Scan(
		&currentVersionID, &currentHash, &currentCompiler, &currentSchema,
		&currentNativeLocator, &currentNativeHash,
	)
	currentNativeSources, nativeSourcesErr := loadVersionNativeSourcesTx(ctx, tx, currentVersionID)
	if nativeSourcesErr != nil {
		return ScriptDetail{}, nativeSourcesErr
	}
	currentNativeDialogueRegions, nativeDialogueRegionsErr := loadVersionNativeDialogueRegionsTx(ctx, tx, currentVersionID)
	if nativeDialogueRegionsErr != nil {
		return ScriptDetail{}, nativeDialogueRegionsErr
	}
	currentIdentifiers, identifierErr := loadVersionIdentifiersTx(ctx, tx, currentVersionID)
	if identifierErr != nil {
		return ScriptDetail{}, identifierErr
	}
	wantedIdentifiers := indexedScriptIdentifiers(compilation.Analysis)
	if err == nil && currentHash == contentHash &&
		currentCompiler == scriptcontent.YarnCompilerVersion &&
		currentSchema == scriptcontent.YarnCommandSchemaVersion &&
		currentNativeLocator == input.SourceLocator && currentNativeHash == input.SourceHash &&
		nativeSourceReferencesEqual(currentNativeSources, nativeSources) &&
		nativeDialogueRegionReferencesEqual(currentNativeDialogueRegions, nativeDialogueRegions) &&
		identifierUsagesEqual(currentIdentifiers, wantedIdentifiers) {
		if err := syncOfficialTestFixtures(ctx, tx, scriptID, currentVersionID, testFixtures); err != nil {
			return ScriptDetail{}, err
		}
		if err := tx.Commit(); err != nil {
			return ScriptDetail{}, err
		}
		return s.Script(ctx, 0, scriptID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ScriptDetail{}, err
	}
	var nextVersion int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM script_versions WHERE script_id=$1`, scriptID).Scan(&nextVersion); err != nil {
		return ScriptDetail{}, err
	}
	var versionID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO script_versions (
		script_id,version,status,content_format,schema_version,source_text,
		source_text_hash,compiled_program,compiled_program_hash,compiler_version,
		command_schema_version,compile_status,compiler_diagnostics,compiler_lines,
		compiler_nodes,summary,source_hash,native_source_locator,native_source_hash
	) VALUES ($1,$2,'draft','yarn',$3,$4,$5,$6,$7,$8,$9,'valid',$10,$11,$12,$13,$14,$15,$16)
	RETURNING id`, scriptID, nextVersion, scriptcontent.YarnContentSchema,
		canonicalSource, scriptcontent.SourceHash(canonicalSource), compilation.Program,
		scriptcontent.BytesHash(compilation.Program), scriptcontent.YarnCompilerVersion,
		scriptcontent.YarnCommandSchemaVersion, diagnostics, compilerLines, compilerNodes,
		input.Summary, contentHash, input.SourceLocator, input.SourceHash).Scan(&versionID)
	if err != nil {
		return ScriptDetail{}, err
	}
	if err := replaceScriptIndexes(ctx, tx, versionID, compilation.Analysis); err != nil {
		return ScriptDetail{}, err
	}
	if err := insertVersionNativeSources(ctx, tx, versionID, nativeSources); err != nil {
		return ScriptDetail{}, err
	}
	if err := insertVersionNativeDialogueRegions(ctx, tx, versionID, nativeDialogueRegions); err != nil {
		return ScriptDetail{}, err
	}
	if currentVersionID != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE script_versions SET status='superseded' WHERE id=$1 AND status='published'`, currentVersionID); err != nil {
			return ScriptDetail{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE script_versions SET status='review',updated_at=now() WHERE id=$1`, versionID); err != nil {
		return ScriptDetail{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE script_versions SET status='published',published_at=now(),updated_at=now() WHERE id=$1`, versionID); err != nil {
		return ScriptDetail{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scripts SET current_published_version_id=$2,updated_at=now() WHERE id=$1`, scriptID, versionID); err != nil {
		return ScriptDetail{}, err
	}
	if err := syncOfficialTestFixtures(ctx, tx, scriptID, versionID, testFixtures); err != nil {
		return ScriptDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptDetail{}, err
	}
	return s.Script(ctx, 0, scriptID)
}

func normalizeOfficialTestFixtures(fixtures []OfficialScriptTestFixture, nodes []scriptcontent.CompiledNode) ([]OfficialScriptTestFixture, error) {
	knownNodes := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		knownNodes[node.Title] = true
	}
	seen := make(map[string]bool, len(fixtures))
	result := make([]OfficialScriptTestFixture, len(fixtures))
	for index, fixture := range fixtures {
		fixture.Name = strings.TrimSpace(fixture.Name)
		fixture.Description = strings.TrimSpace(fixture.Description)
		fixture.StartNode = strings.TrimSpace(fixture.StartNode)
		key := strings.ToLower(fixture.Name)
		if len([]rune(fixture.Name)) < 1 || len([]rune(fixture.Name)) > 120 || seen[key] {
			return nil, fmt.Errorf("official test fixture %d has an invalid or duplicate name", index)
		}
		if len([]rune(fixture.Description)) > 1000 {
			return nil, fmt.Errorf("official test fixture %q description is too long", fixture.Name)
		}
		if !knownNodes[fixture.StartNode] {
			return nil, fmt.Errorf("official test fixture %q refers to unknown node %q", fixture.Name, fixture.StartNode)
		}
		if len(fixture.Fixture) == 0 || len(fixture.Fixture) > 64*1024 {
			return nil, fmt.Errorf("official test fixture %q has invalid JSON size", fixture.Name)
		}
		var object map[string]any
		if err := json.Unmarshal(fixture.Fixture, &object); err != nil || object == nil {
			return nil, fmt.Errorf("official test fixture %q must contain a JSON object", fixture.Name)
		}
		seen[key] = true
		result[index] = fixture
	}
	return result, nil
}

func syncOfficialTestFixtures(ctx context.Context, tx *sql.Tx, scriptID, versionID int64, fixtures []OfficialScriptTestFixture) error {
	wanted := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		key := strings.ToLower(fixture.Name)
		wanted[key] = true
		var fixtureID int64
		var origin string
		err := tx.QueryRowContext(ctx, `SELECT id,origin FROM script_test_fixtures
			WHERE script_id=$1 AND lower(name)=$2 ORDER BY archived_at NULLS FIRST,id LIMIT 1
			FOR UPDATE`, scriptID, key).Scan(&fixtureID, &origin)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			_, err = tx.ExecContext(ctx, `INSERT INTO script_test_fixtures
				(script_id,source_version_id,name,description,start_node,fixture,origin)
				VALUES ($1,$2,$3,$4,$5,$6,'official')`, scriptID, versionID,
				fixture.Name, fixture.Description, fixture.StartNode, fixture.Fixture)
		case err != nil:
			return err
		case origin != "official":
			return fmt.Errorf("official test fixture %q conflicts with a community fixture", fixture.Name)
		default:
			_, err = tx.ExecContext(ctx, `UPDATE script_test_fixtures SET
				source_version_id=$2,name=$3,description=$4,start_node=$5,fixture=$6,
				revision=revision+1,updated_at=now()
				WHERE id=$1 AND (source_version_id, name, description, start_node, fixture)
				IS DISTINCT FROM ($2,$3,$4,$5,$6::jsonb)`, fixtureID, versionID,
				fixture.Name, fixture.Description, fixture.StartNode, fixture.Fixture)
		}
		if err != nil {
			return fmt.Errorf("sync official test fixture %q: %w", fixture.Name, err)
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,lower(name) FROM script_test_fixtures
		WHERE script_id=$1 AND origin='official' AND archived_at IS NULL FOR UPDATE`, scriptID)
	if err != nil {
		return err
	}
	type existingFixture struct {
		id   int64
		name string
	}
	existing := []existingFixture{}
	for rows.Next() {
		var fixture existingFixture
		if err := rows.Scan(&fixture.id, &fixture.name); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, fixture)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, fixture := range existing {
		if wanted[fixture.name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE script_test_fixtures SET
			archived_at=now(),archived_by=NULL,revision=revision+1,updated_at=now()
			WHERE id=$1`, fixture.id); err != nil {
			return err
		}
	}
	return nil
}

func loadVersionIdentifiersTx(ctx context.Context, tx *sql.Tx, versionID int64) ([]scriptcontent.IdentifierUsage, error) {
	if versionID == 0 {
		return []scriptcontent.IdentifierUsage{}, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,identifier
		FROM script_version_identifiers WHERE version_id=$1 ORDER BY kind,identifier`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []scriptcontent.IdentifierUsage{}
	for rows.Next() {
		var identifier scriptcontent.IdentifierUsage
		if err := rows.Scan(&identifier.Kind, &identifier.Identifier); err != nil {
			return nil, err
		}
		result = append(result, identifier)
	}
	return result, rows.Err()
}

func identifierUsagesEqual(left, right []scriptcontent.IdentifierUsage) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeOfficialNativeSources(input OfficialYarnImport) ([]NativeSourceReference, error) {
	sources := append([]NativeSourceReference(nil), input.NativeSources...)
	if len(sources) == 0 {
		sources = []NativeSourceReference{{
			Ordinal: 0, Role: "primary", Locator: input.SourceLocator, Hash: input.SourceHash,
		}}
	}
	seenRolesAndLocators := make(map[string]struct{}, len(sources))
	for index := range sources {
		sources[index].Ordinal = index
		sources[index].Role = strings.TrimSpace(sources[index].Role)
		sources[index].Locator = strings.TrimSpace(sources[index].Locator)
		sources[index].Hash = strings.TrimSpace(sources[index].Hash)
		if !nativeSourceRolePattern.MatchString(sources[index].Role) || sources[index].Locator == "" ||
			!scriptSHA256Pattern.MatchString(sources[index].Hash) {
			return nil, fmt.Errorf("official native source %d requires a role, locator, and SHA-256 hash", index)
		}
		key := sources[index].Role + "\x00" + sources[index].Locator
		if _, duplicate := seenRolesAndLocators[key]; duplicate {
			return nil, fmt.Errorf("official native source %d duplicates role and locator", index)
		}
		seenRolesAndLocators[key] = struct{}{}
	}
	return sources, nil
}

func insertVersionNativeSources(ctx context.Context, tx *sql.Tx, versionID int64, sources []NativeSourceReference) error {
	for _, source := range sources {
		if _, err := tx.ExecContext(ctx, `INSERT INTO script_version_native_sources
			(version_id,ordinal,role,source_locator,source_hash) VALUES ($1,$2,$3,$4,$5)`,
			versionID, source.Ordinal, source.Role, source.Locator, source.Hash); err != nil {
			return fmt.Errorf("insert script native source: %w", err)
		}
	}
	return nil
}

func loadVersionNativeSourcesTx(ctx context.Context, tx *sql.Tx, versionID int64) ([]NativeSourceReference, error) {
	if versionID == 0 {
		return []NativeSourceReference{}, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT ordinal,role,source_locator,source_hash
		FROM script_version_native_sources WHERE version_id=$1 ORDER BY ordinal`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []NativeSourceReference{}
	for rows.Next() {
		var source NativeSourceReference
		if err := rows.Scan(&source.Ordinal, &source.Role, &source.Locator, &source.Hash); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func nativeSourceReferencesEqual(left, right []NativeSourceReference) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeOfficialNativeDialogueRegions(regions []NativeDialogueRegionReference) ([]NativeDialogueRegionReference, error) {
	result := append([]NativeDialogueRegionReference(nil), regions...)
	seen := make(map[string]struct{}, len(result))
	for index := range result {
		region := &result[index]
		region.Ordinal = index
		region.Area = strings.ToUpper(strings.TrimSpace(region.Area))
		region.Ownership = strings.TrimSpace(region.Ownership)
		region.ActivityID = strings.TrimSpace(region.ActivityID)
		region.EvidenceLocator = strings.TrimSpace(region.EvidenceLocator)
		if region.Disc < 1 || region.Disc > 3 || !nativeDialogueAreaPattern.MatchString(region.Area) ||
			region.ExecutableTargetIndex < 0 || region.RegionStartFileOffset < 0 ||
			region.EvidenceLocator == "" {
			return nil, fmt.Errorf("official native dialogue region %d has invalid identity or evidence", index)
		}
		if region.Ownership != "translated" && region.Ownership != "specialized-activity-owned" {
			return nil, fmt.Errorf("official native dialogue region %d has invalid ownership", index)
		}
		if (region.Ownership == "translated") != (region.ActivityID == "") {
			return nil, fmt.Errorf("official native dialogue region %d has an invalid activity boundary", index)
		}
		key := fmt.Sprintf("%d:%s:%d:%d", region.Disc, region.Area,
			region.ExecutableTargetIndex, region.RegionStartFileOffset)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("official native dialogue region %d duplicates %s", index, key)
		}
		seen[key] = struct{}{}
	}
	return result, nil
}

func insertVersionNativeDialogueRegions(ctx context.Context, tx *sql.Tx, versionID int64, regions []NativeDialogueRegionReference) error {
	for _, region := range regions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO script_version_native_dialogue_regions
			(version_id,ordinal,disc,area,executable_target_index,region_start_file_offset,
			 ownership,activity_id,evidence_locator)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9)`, versionID,
			region.Ordinal, region.Disc, region.Area, region.ExecutableTargetIndex,
			region.RegionStartFileOffset, region.Ownership, region.ActivityID,
			region.EvidenceLocator); err != nil {
			return fmt.Errorf("insert script native dialogue region: %w", err)
		}
	}
	return nil
}

func loadVersionNativeDialogueRegionsTx(ctx context.Context, tx *sql.Tx, versionID int64) ([]NativeDialogueRegionReference, error) {
	if versionID == 0 {
		return []NativeDialogueRegionReference{}, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT ordinal,disc,area,executable_target_index,
		region_start_file_offset,ownership,COALESCE(activity_id,''),evidence_locator
		FROM script_version_native_dialogue_regions WHERE version_id=$1 ORDER BY ordinal`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	regions := []NativeDialogueRegionReference{}
	for rows.Next() {
		var region NativeDialogueRegionReference
		if err := rows.Scan(&region.Ordinal, &region.Disc, &region.Area,
			&region.ExecutableTargetIndex, &region.RegionStartFileOffset,
			&region.Ownership, &region.ActivityID, &region.EvidenceLocator); err != nil {
			return nil, err
		}
		regions = append(regions, region)
	}
	return regions, rows.Err()
}

func nativeDialogueRegionReferencesEqual(left, right []NativeDialogueRegionReference) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func diagnosticSummary(diagnostics []scriptcontent.Diagnostic) string {
	if len(diagnostics) == 0 {
		return "compiler returned no program"
	}
	parts := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		parts = append(parts, fmt.Sprintf("%s line %d: %s", diagnostic.Code, diagnostic.Line, diagnostic.Message))
	}
	return strings.Join(parts, "; ")
}
