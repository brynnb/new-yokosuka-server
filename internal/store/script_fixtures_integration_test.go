package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestPostgresScriptTestFixtureLifecycle(t *testing.T) {
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
	database.SetScriptCompiler(integrationYarnCompiler{})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	passwordHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	owner, err := database.CreateRegisteredAccount(ctx, "fixture-owner-"+suffix+"@example.test", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	contributor, err := database.CreateRegisteredAccount(ctx, "fixture-contributor-"+suffix+"@example.test", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	var scriptID int64
	t.Cleanup(func() {
		if scriptID != 0 {
			_, _ = database.db.Exec(`UPDATE scripts SET current_published_version_id=NULL,current_reference_version_id=NULL WHERE id=$1`, scriptID)
			_, _ = database.db.Exec(`DELETE FROM scripts WHERE id=$1`, scriptID)
		}
		_, _ = database.db.Exec(`DELETE FROM accounts WHERE id=ANY($1)`, []int64{owner.ID, contributor.ID})
	})

	detail, err := database.CreateYarnScript(ctx, owner.ID, YarnScriptCreateInput{
		Slug: "fixture-lifecycle-" + suffix, Title: "Fixture lifecycle",
		SourceText: "title: Start\n---\nRyo: Hello\n===\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	scriptID = detail.ID
	version := detail.Versions[0]
	fixtureJSON, _ := json.Marshal(map[string]any{
		"scene": "D000", "yen": 100,
		"flags": map[string]bool{"story.ready": true},
	})
	created, err := database.CreateScriptTestFixture(ctx, owner.ID, scriptID, ScriptTestFixtureInput{
		SourceVersionID: version.ID, Name: "Ready to talk", Description: "Initial state",
		StartNode: "Start", Fixture: fixtureJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.SourceVersionNumber != 1 || created.CreatedBy == nil || *created.CreatedBy != owner.ID {
		t.Fatalf("unexpected created fixture: %#v", created)
	}
	if _, err := database.CreateScriptTestFixture(ctx, contributor.ID, scriptID, ScriptTestFixtureInput{
		SourceVersionID: version.ID, Name: "Hidden draft", StartNode: "Start", Fixture: fixtureJSON,
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("draft fixture creation err=%v, want forbidden", err)
	}

	updated, err := database.UpdateScriptTestFixture(ctx, owner.ID, scriptID, created.ID, ScriptTestFixtureInput{
		SourceVersionID: version.ID, Name: "Ready after clue", Description: "Changed",
		StartNode: "Start", Fixture: fixtureJSON, Revision: created.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Name != "Ready after clue" {
		t.Fatalf("unexpected update: %#v", updated)
	}
	if _, err := database.UpdateScriptTestFixture(ctx, owner.ID, scriptID, created.ID, ScriptTestFixtureInput{
		SourceVersionID: version.ID, Name: "Stale", StartNode: "Start",
		Fixture: fixtureJSON, Revision: created.Revision,
	}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale fixture update err=%v", err)
	}
	archived, err := database.SetScriptTestFixtureArchived(ctx, owner.ID, scriptID, created.ID, updated.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	active, err := database.ListScriptTestFixtures(ctx, owner.ID, scriptID, false)
	if err != nil || len(active) != 0 {
		t.Fatalf("active fixtures=%#v err=%v", active, err)
	}
	all, err := database.ListScriptTestFixtures(ctx, owner.ID, scriptID, true)
	if err != nil || len(all) != 1 || all[0].ArchivedAt == nil {
		t.Fatalf("archived fixtures=%#v err=%v", all, err)
	}
	restored, err := database.SetScriptTestFixtureArchived(ctx, owner.ID, scriptID, created.ID, archived.Revision, false)
	if err != nil || restored.ArchivedAt != nil {
		t.Fatalf("restored=%#v err=%v", restored, err)
	}

	if _, err := database.db.ExecContext(ctx, `UPDATE accounts SET role='moderator' WHERE id=$1`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SubmitScriptVersion(ctx, owner.ID, scriptID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.PublishScriptVersion(ctx, owner.ID, scriptID, 1); err != nil {
		t.Fatal(err)
	}
	shared, err := database.CreateScriptTestFixture(ctx, contributor.ID, scriptID, ScriptTestFixtureInput{
		SourceVersionID: version.ID, Name: "Community branch", StartNode: "Start", Fixture: fixtureJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := database.ListScriptTestFixtures(ctx, 0, scriptID, false)
	if err != nil || len(visible) != 2 {
		t.Fatalf("public fixtures=%#v err=%v", visible, err)
	}
	if _, err := database.SetScriptTestFixtureArchived(ctx, owner.ID, scriptID, shared.ID, shared.Revision, true); err != nil {
		t.Fatalf("moderator archive: %v", err)
	}
}
