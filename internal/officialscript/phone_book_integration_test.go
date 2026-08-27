package officialscript

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func TestD000TelephoneBookImportsAsSpecializedActivityAndExecutesFromPostgres(t *testing.T) {
	databaseURL := os.Getenv("NEW_YOKOSUKA_TEST_DATABASE_URL")
	compilerPath := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if databaseURL == "" || compilerPath == "" {
		t.Skip("PostgreSQL and the official Yarn compiler are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	database, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	compiler, err := scriptcontent.NewProcessCompiler(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	database.SetScriptCompiler(compiler)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	definition, _ := Lookup("d000-telephone-book")
	definition.Slug = "telephone-book-runtime-test-" + suffix
	definition.Title = "Telephone-book runtime test " + suffix
	definition.NativeSources[0].Locator += "?integration=" + suffix
	definition.Triggers[0].Object = "TBK1_TEST_" + suffix
	detail, err := database.ImportOfficialYarnScript(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.DB().Exec(`UPDATE scripts SET current_published_version_id=NULL WHERE id=$1`, detail.ID)
		_, _ = database.DB().Exec(`DELETE FROM scripts WHERE id=$1`, detail.ID)
	})
	version := detail.Versions[0]
	if len(version.NativeSources) != 6 || len(version.Analysis.Triggers) != 1 ||
		len(version.NativeDialogueRegions) != 1 ||
		version.NativeDialogueRegions[0].Ownership != "specialized-activity-owned" ||
		version.NativeDialogueRegions[0].ActivityID != "d000.telephone-book.native" {
		t.Fatalf("telephone-book import metadata=%#v", version)
	}
	foundActivity := false
	for _, identifier := range version.Analysis.Identifiers {
		if identifier == (scriptcontent.IdentifierUsage{
			Kind: "activity", Identifier: "d000.telephone-book.native",
		}) {
			foundActivity = true
		}
	}
	if !foundActivity {
		t.Fatalf("telephone-book activity identifier missing: %#v", version.Analysis.Identifiers)
	}
	assertOfficialFixtureCount(t, ctx, database, detail.ID, version.ID, 1)

	account, err := database.CreateRegisteredAccount(
		ctx, "telephone-book-runtime-"+suffix+"@example.test",
		"$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.DB().Exec(`DELETE FROM accounts WHERE id=$1`, account.ID) })
	character, err := database.CreateCharacter(ctx, account.ID, "Book"+suffix, "ryo", "D000", 0, 0, 0, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := scriptruntime.NewBridge(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	previewOfficialFixtures(t, ctx, database, bridge, detail.ID, version, map[string]string{
		"Use Dobuita telephone book": "complete",
	})
	engine, err := scriptevent.New(database, bridge)
	if err != nil {
		t.Fatal(err)
	}
	yield, err := engine.Start(ctx, account.ID, character.ID, scriptcontent.TriggerSelector{
		Kind: "use", Area: "D000", Object: definition.Triggers[0].Object,
	}, scriptevent.WorldFacts{})
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, yield, "command", "start_activity")
	if len(yield.RuntimeEvent.Arguments) != 1 ||
		yield.RuntimeEvent.Arguments[0].Value == nil ||
		*yield.RuntimeEvent.Arguments[0].Value != "d000.telephone-book.native" {
		t.Fatalf("telephone-book activity yield=%#v", yield.RuntimeEvent)
	}
	yield, err = engine.Advance(ctx, account.ID, character.ID, yield.RunID, scriptruntime.Continue())
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, yield, "complete", "")
	if yield.State == nil || len(yield.State.Flags) != 0 || len(yield.State.Inventory) != 0 {
		t.Fatalf("presentation-only activity committed unexpected durable state: %#v", yield.State)
	}
}
