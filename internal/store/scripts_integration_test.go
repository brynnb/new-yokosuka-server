package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

func integrationScriptDocument(line string) scriptcontent.Document {
	return scriptcontent.Document{
		Schema:      scriptcontent.Schema,
		EntryNodeID: "start",
		Nodes: []scriptcontent.Node{
			{ID: "start", Type: "trigger", Label: "Talk", Config: map[string]any{"kind": "talk", "area": "D000", "actor": "INE_"}},
			{ID: "line", Type: "dialogue", Label: "Line", Config: map[string]any{"speaker": "INE_", "text": line}},
			{ID: "done", Type: "end", Label: "Done", Config: map[string]any{"kind": "complete"}},
		},
		Edges: []scriptcontent.Edge{
			{ID: "start-line", From: "start", To: "line"},
			{ID: "line-done", From: "line", To: "done"},
		},
	}
}

type integrationYarnCompiler struct{}

func (integrationYarnCompiler) Compile(_ context.Context, _ string, source string) (scriptcontent.Compilation, error) {
	if strings.Contains(source, "<<broken>>") {
		return scriptcontent.Compilation{
			Diagnostics: []scriptcontent.Diagnostic{{Severity: "error", Code: "TEST0001", Message: "broken fixture"}},
			Nodes:       []scriptcontent.CompiledNode{{Title: "Start"}},
		}, nil
	}
	line := "Ryo: Fixture"
	return scriptcontent.Compilation{
		Valid:   true,
		Program: []byte("compiled:" + source),
		Lines:   []scriptcontent.CompiledLine{{ID: "line:test", Text: &line, NodeName: "Start"}},
		Nodes:   []scriptcontent.CompiledNode{{Title: "Start"}},
		Analysis: scriptcontent.Analysis{
			Dependencies: []scriptcontent.Dependency{{Access: "write", Kind: "flag", Identifier: "integration.met"}},
			Identifiers:  []scriptcontent.IdentifierUsage{{Kind: "camera", Identifier: "d000.hato.camera.2950"}},
			Triggers:     []scriptcontent.Trigger{},
			Warnings:     []string{},
		},
	}, nil
}

func TestPostgresScriptRepositoryLifecycle(t *testing.T) {
	databaseURL := os.Getenv("NEW_YOKOSUKA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEW_YOKOSUKA_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if compilerPath := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER"); compilerPath != "" {
		compiler, err := scriptcontent.NewProcessCompiler(compilerPath)
		if err != nil {
			t.Fatal(err)
		}
		database.SetScriptCompiler(compiler)
	} else {
		database.SetScriptCompiler(integrationYarnCompiler{})
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	passwordHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := database.CreateRegisteredAccount(ctx, "script-owner-"+suffix+"@example.test", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	contributor, err := database.CreateRegisteredAccount(ctx, "script-contributor-"+suffix+"@example.test", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := database.CreateRegisteredAccount(ctx, "script-outsider-"+suffix+"@example.test", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	var scriptIDs []int64
	t.Cleanup(func() {
		for _, scriptID := range scriptIDs {
			if _, err := database.db.Exec(`UPDATE scripts SET current_published_version_id=NULL,current_reference_version_id=NULL WHERE id=$1`, scriptID); err != nil {
				t.Errorf("clear script version pointers: %v", err)
			}
			if _, err := database.db.Exec(`DELETE FROM scripts WHERE id=$1`, scriptID); err != nil {
				t.Errorf("delete script fixture: %v", err)
			}
		}
		for _, accountID := range []int64{owner.ID, contributor.ID, outsider.ID} {
			if _, err := database.db.Exec(`DELETE FROM accounts WHERE id=$1`, accountID); err != nil {
				t.Errorf("delete account fixture: %v", err)
			}
		}
	})

	yarnCreated, err := database.CreateYarnScript(ctx, owner.ID, YarnScriptCreateInput{
		Slug: "integration-yarn-" + suffix, Title: "Integration Yarn script",
		Description: "Yarn lifecycle test", Summary: "Initial Yarn source",
		SourceText: "title: Start\r\n---\r\nRyo: Hello\r\n<<start_camera \"d000.hato.camera.2950\">>\r\n<<set_flag \"integration.met\">>\r\n===",
		Triggers:   []scriptcontent.Trigger{{NodeID: "Start", Kind: "talk", Area: "D000", Actor: "HATO", Priority: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	scriptIDs = append(scriptIDs, yarnCreated.ID)
	if len(yarnCreated.Versions) != 1 || yarnCreated.Versions[0].ContentFormat != "yarn" ||
		yarnCreated.Versions[0].CompileStatus != "valid" ||
		yarnCreated.Versions[0].SourceText != "title: Start\n---\nRyo: Hello\n<<start_camera \"d000.hato.camera.2950\">>\n<<set_flag \"integration.met\">>\n===\n" ||
		len(yarnCreated.Versions[0].SourceTextHash) != 64 ||
		len(yarnCreated.Versions[0].CompiledProgramHash) != 64 ||
		len(yarnCreated.Versions[0].CompiledProgram) == 0 || yarnCreated.Versions[0].Document != nil ||
		len(yarnCreated.Versions[0].CompilerLines) != 1 || len(yarnCreated.Versions[0].CompilerNodes) != 1 ||
		len(yarnCreated.Versions[0].Analysis.Dependencies) != 1 ||
		len(yarnCreated.Versions[0].Analysis.Triggers) != 1 || yarnCreated.Versions[0].Analysis.Triggers[0].Priority != 10 {
		t.Fatalf("unexpected created Yarn script: %#v", yarnCreated)
	}
	collaborator, err := database.SetScriptCollaborator(
		ctx, owner.ID, yarnCreated.ID, contributor.Email, "editor",
	)
	if err != nil || collaborator.AccountID != contributor.ID || collaborator.Role != "editor" {
		t.Fatalf("added collaborator=%#v err=%v", collaborator, err)
	}
	if _, err := database.SetScriptCollaborator(
		ctx, contributor.ID, yarnCreated.ID, outsider.Email, "reviewer",
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor managed collaborators err=%v, want ErrForbidden", err)
	}
	collaborator, err = database.SetScriptCollaborator(
		ctx, owner.ID, yarnCreated.ID, contributor.Email, "reviewer",
	)
	if err != nil || collaborator.Role != "reviewer" {
		t.Fatalf("updated collaborator=%#v err=%v", collaborator, err)
	}
	collaborators, err := database.ListScriptCollaborators(ctx, contributor.ID, yarnCreated.ID)
	if err != nil || len(collaborators) != 2 || collaborators[0].Role != "owner" ||
		collaborators[1].AccountID != contributor.ID || collaborators[1].Role != "reviewer" {
		t.Fatalf("collaborators=%#v err=%v", collaborators, err)
	}
	if _, err := database.CreateScriptVersion(
		ctx, contributor.ID, yarnCreated.ID, 1,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reviewer created version err=%v, want ErrForbidden", err)
	}
	if err := database.RemoveScriptCollaborator(ctx, owner.ID, yarnCreated.ID, contributor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ListScriptCollaborators(ctx, contributor.ID, yarnCreated.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("removed collaborator retained access err=%v", err)
	}
	if _, err := database.SetScriptCollaborator(
		ctx, owner.ID, yarnCreated.ID, owner.Email, "editor",
	); err == nil {
		t.Fatal("owner role could be replaced through collaborator CRUD")
	}
	yarnSaved, err := database.SaveYarnScriptDraft(ctx, owner.ID, yarnCreated.ID, 1, YarnDraftUpdateInput{
		Revision: 1, Summary: "Changed Yarn source",
		SourceText: "title: Start\n---\nRyo: I understand.\n<<start_camera \"d000.hato.camera.2950\">>\n<<set_flag \"integration.met\">>\n===\n",
		Triggers:   []scriptcontent.Trigger{{NodeID: "Start", Kind: "talk", Area: "D000", Actor: "HATO", Priority: 20}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if yarnSaved.Revision != 2 || yarnSaved.CompileStatus != "valid" ||
		!strings.Contains(yarnSaved.SourceText, "I understand") || len(yarnSaved.Analysis.Dependencies) != 1 ||
		len(yarnSaved.Analysis.Triggers) != 1 || yarnSaved.Analysis.Triggers[0].Priority != 20 {
		t.Fatalf("unexpected saved Yarn version: %#v", yarnSaved)
	}
	yarnFork, err := database.CreateScriptVersion(ctx, owner.ID, yarnCreated.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if yarnFork.Version != 2 || len(yarnFork.Analysis.Dependencies) != 1 ||
		len(yarnFork.Analysis.Triggers) != 1 || yarnFork.Analysis.Triggers[0].Priority != 20 {
		t.Fatalf("Yarn indexes were not copied to the new version: %#v", yarnFork)
	}
	if _, err := database.SubmitScriptVersion(ctx, owner.ID, yarnCreated.ID, yarnFork.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE script_versions SET source_text='mutated' WHERE id=$1`, yarnFork.ID); err == nil {
		t.Fatal("review Yarn source was mutable outside the repository API")
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE script_versions SET status='draft' WHERE id=$1`, yarnFork.ID); err == nil {
		t.Fatal("review version could transition back to a mutable draft")
	}
	yarnRevision, err := database.CreateScriptVersion(ctx, owner.ID, yarnCreated.ID, yarnFork.Version)
	if err != nil {
		t.Fatal(err)
	}
	if yarnRevision.Version != 3 || yarnRevision.Status != "draft" ||
		yarnRevision.BasedOnVersion == nil || *yarnRevision.BasedOnVersion != yarnFork.ID {
		t.Fatalf("review version was not revised as an immutable child draft: %#v", yarnRevision)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE script_versions SET command_schema_version='invalid-command-schema' WHERE id=$1`, yarnRevision.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SubmitScriptVersion(ctx, owner.ID, yarnCreated.ID, yarnRevision.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale-schema submission err=%v, want rejection", err)
	}
	yarnStaleReview, err := database.CreateScriptVersion(ctx, owner.ID, yarnCreated.ID, yarnFork.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE script_versions SET command_schema_version='invalid-command-schema',status='review' WHERE id=$1`, yarnStaleReview.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE script_version_triggers SET priority=21 WHERE version_id=$1`, yarnFork.ID); err == nil {
		t.Fatal("review trigger metadata was mutable")
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE script_version_identifiers SET identifier='changed' WHERE version_id=$1`, yarnFork.ID); err == nil {
		t.Fatal("review identifier metadata was mutable")
	}
	yarnInvalid, err := database.SaveYarnScriptDraft(ctx, owner.ID, yarnCreated.ID, 1, YarnDraftUpdateInput{
		Revision: 2, Summary: "Invalid Yarn source",
		SourceText: "title: Start\n---\n<<broken>>\n===\n",
		Triggers:   []scriptcontent.Trigger{{NodeID: "Start", Kind: "talk", Area: "D000", Actor: "HATO"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if yarnInvalid.Revision != 3 || yarnInvalid.CompileStatus != "invalid" ||
		len(yarnInvalid.CompiledProgram) != 0 || len(yarnInvalid.CompilerDiagnostics) != 1 {
		t.Fatalf("unexpected invalid Yarn version: %#v", yarnInvalid)
	}
	if _, err := database.SubmitScriptVersion(ctx, owner.ID, yarnCreated.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid Yarn submission err = %v, want rejection", err)
	}

	if _, err := database.db.ExecContext(ctx, `UPDATE accounts SET role='moderator' WHERE id=$1`, owner.ID); err != nil {
		t.Fatal(err)
	}
	yarnPublished, err := database.PublishScriptVersion(ctx, owner.ID, yarnCreated.ID, yarnFork.Version)
	if err != nil {
		t.Fatal(err)
	}
	if yarnPublished.Status != "published" {
		t.Fatalf("unexpected published Yarn version: %#v", yarnPublished)
	}
	if _, err := database.PublishScriptVersion(ctx, owner.ID, yarnCreated.ID, yarnStaleReview.Version); err == nil || !strings.Contains(err.Error(), "current compiler and command schema") {
		t.Fatalf("stale-schema review publication err=%v, want current-schema rejection", err)
	}
	newerDraft, err := database.CreateScriptVersion(ctx, owner.ID, yarnCreated.ID, yarnPublished.Version)
	if err != nil {
		t.Fatal(err)
	}
	newerDraft, err = database.SaveYarnScriptDraft(ctx, owner.ID, yarnCreated.ID, newerDraft.Version, YarnDraftUpdateInput{
		Revision: newerDraft.Revision, Summary: "Newer publication",
		SourceText: "title: Start\n---\nRyo: This newer version will be rolled back.\n<<start_camera \"d000.hato.camera.2950\">>\n<<set_flag \"integration.met\">>\n===\n",
		Triggers:   []scriptcontent.Trigger{{NodeID: "Start", Kind: "talk", Area: "D000", Actor: "HATO", Priority: 30}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SubmitScriptVersion(ctx, owner.ID, yarnCreated.ID, newerDraft.Version); err != nil {
		t.Fatal(err)
	}
	newerPublished, err := database.PublishScriptVersion(ctx, owner.ID, yarnCreated.ID, newerDraft.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RollbackScriptVersion(ctx, outsider.ID, yarnCreated.ID, yarnPublished.Version); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member rollback err=%v, want ErrForbidden", err)
	}
	rolledBack, err := database.RollbackScriptVersion(ctx, owner.ID, yarnCreated.ID, yarnPublished.Version)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Status != "published" || rolledBack.Version != newerPublished.Version+1 ||
		rolledBack.BasedOnVersion == nil || *rolledBack.BasedOnVersion != yarnPublished.ID ||
		rolledBack.SourceText != yarnPublished.SourceText ||
		rolledBack.CommandSchemaVersion != scriptcontent.YarnCommandSchemaVersion ||
		!strings.HasPrefix(rolledBack.Summary, "Rollback to version ") ||
		len(rolledBack.Analysis.Triggers) != 1 || rolledBack.Analysis.Triggers[0].Priority != 20 {
		t.Fatalf("unexpected rollback version: %#v", rolledBack)
	}
	var rollbackPointer int64
	var priorStatus, rollbackTargetStatus string
	if err := database.db.QueryRowContext(ctx, `
		SELECT s.current_published_version_id,newer.status,target.status
		FROM scripts s
		JOIN script_versions newer ON newer.id=$2
		JOIN script_versions target ON target.id=$3
		WHERE s.id=$1
	`, yarnCreated.ID, newerPublished.ID, yarnPublished.ID).Scan(
		&rollbackPointer, &priorStatus, &rollbackTargetStatus,
	); err != nil {
		t.Fatal(err)
	}
	if rollbackPointer != rolledBack.ID || priorStatus != "superseded" || rollbackTargetStatus != "superseded" {
		t.Fatalf("rollback pointer=%d prior=%q target=%q", rollbackPointer, priorStatus, rollbackTargetStatus)
	}
	identifierCatalog, err := database.ListPublishedScriptIdentifiers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantCatalog := map[string]bool{
		"actor:HATO": true, "camera:d000.hato.camera.2950": true,
		"flag:integration.met": true, "scene:D000": true,
	}
	for _, identifier := range identifierCatalog {
		delete(wantCatalog, identifier.Kind+":"+identifier.Identifier)
	}
	if len(wantCatalog) != 0 {
		t.Fatalf("published identifier catalog missing %#v: %#v", wantCatalog, identifierCatalog)
	}
	publicView, err := database.Script(ctx, 0, yarnCreated.ID)
	if err != nil || publicView.CurrentVersion == nil || publicView.CurrentVersion.Status != "published" {
		t.Fatalf("public script view: %#v, %v", publicView, err)
	}

	communityDraft, err := database.CreateScriptVersion(ctx, contributor.ID, yarnCreated.ID, rolledBack.Version)
	if err != nil {
		t.Fatal(err)
	}
	if communityDraft.Version != rolledBack.Version+1 || communityDraft.Status != "draft" ||
		communityDraft.ContentFormat != "yarn" || communityDraft.SourceText != rolledBack.SourceText {
		t.Fatalf("unexpected community draft: %#v", communityDraft)
	}
	if _, err := database.CreateScriptVersion(ctx, outsider.ID, yarnCreated.ID, communityDraft.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private draft fork err = %v, want ErrNotFound", err)
	}

	recoveredInput := RecoveredScriptImport{
		Slug:          "recovered-integration-" + suffix,
		Title:         "Recovered integration script",
		Description:   "Import test",
		SourceLocator: "integration://" + suffix,
		SourceHash:    strings.Repeat("a", 64),
		DocumentHash:  strings.Repeat("b", 64),
		Document:      integrationScriptDocument("Original"),
		Summary:       "Recovered reference",
	}
	recovered, err := database.ImportRecoveredScript(ctx, recoveredInput)
	if err != nil {
		t.Fatal(err)
	}
	scriptIDs = append(scriptIDs, recovered.ID)
	reimported, err := database.ImportRecoveredScript(ctx, recoveredInput)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.CurrentVersion == nil || reimported.CurrentVersion == nil || recovered.CurrentVersion.ID != reimported.CurrentVersion.ID || len(reimported.Versions) != 1 {
		t.Fatalf("recovered import was not idempotent: first=%#v second=%#v", recovered, reimported)
	}

	archived, err := database.SetScriptArchived(ctx, owner.ID, yarnCreated.ID, true)
	if err != nil || archived.ArchivedAt == nil || archived.ArchivedBy == nil || *archived.ArchivedBy != owner.ID {
		t.Fatalf("archived script=%#v err=%v", archived, err)
	}
	if _, err := database.Script(ctx, 0, yarnCreated.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("public archived script err=%v, want ErrNotFound", err)
	}
	if _, err := database.CreateScriptVersion(ctx, owner.ID, yarnCreated.ID, rolledBack.Version); err == nil {
		t.Fatal("archived script accepted a new version")
	}
	if _, err := database.UpdateScriptMetadata(ctx, owner.ID, yarnCreated.ID, ScriptMetadataUpdate{
		Title: "Archived edit", Description: "",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("archived metadata update err=%v, want ErrForbidden", err)
	}
	restored, err := database.SetScriptArchived(ctx, owner.ID, yarnCreated.ID, false)
	if err != nil || restored.ArchivedAt != nil || restored.ArchivedBy != nil {
		t.Fatalf("restored script=%#v err=%v", restored, err)
	}
	if _, err := database.Script(ctx, 0, yarnCreated.ID); err != nil {
		t.Fatalf("restored published script is not public: %v", err)
	}
	yarnEvents, err := database.ListScriptModerationEvents(ctx, owner.ID, yarnCreated.ID)
	if err != nil {
		t.Fatal(err)
	}
	yarnActionCounts := map[string]int{}
	for _, event := range yarnEvents {
		yarnActionCounts[event.Action]++
	}
	for action, minimum := range map[string]int{
		"version.submitted":          2,
		"version.published":          2,
		"version.rollback-published": 1,
		"collaborator.added":         1,
		"collaborator.role-changed":  1,
		"collaborator.removed":       1,
	} {
		if yarnActionCounts[action] < minimum {
			t.Fatalf("Yarn moderation action %q count=%d, want at least %d: %#v", action, yarnActionCounts[action], minimum, yarnEvents)
		}
	}
	if _, err := database.SetScriptArchived(ctx, owner.ID, recovered.ID, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("recovered reference archive err=%v, want ErrForbidden", err)
	}
}
