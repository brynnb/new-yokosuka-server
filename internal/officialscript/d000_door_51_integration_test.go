package officialscript

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func TestDoor51ImportsAndPreviewsEveryNativeBranch(t *testing.T) {
	databaseURL := os.Getenv("NEW_YOKOSUKA_TEST_DATABASE_URL")
	compilerPath := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if databaseURL == "" || compilerPath == "" {
		t.Skip("PostgreSQL and the official Yarn compiler are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	definition, _ := Lookup("d000-door-51-closed-check")
	definition.Slug += "-test-" + suffix
	definition.Title += " test " + suffix
	definition.SourceLocator += "?integration=" + suffix
	definition.Triggers[0].Object += ".test." + suffix
	detail, err := database.ImportOfficialYarnScript(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.DB().Exec(`UPDATE scripts SET current_published_version_id=NULL WHERE id=$1`, detail.ID)
		_, _ = database.DB().Exec(`DELETE FROM scripts WHERE id=$1`, detail.ID)
	})
	if len(detail.Versions) != 1 || len(detail.Versions[0].NativeDialogueRegions) != 1 || len(detail.Versions[0].NativeSources) != 2 {
		t.Fatalf("door 51 provenance=%#v", detail.Versions)
	}
	region := detail.Versions[0].NativeDialogueRegions[0]
	if region.ExecutableTargetIndex != 530 || region.RegionStartFileOffset != 0x7507c || region.Ownership != "translated" {
		t.Fatalf("door 51 region=%#v", region)
	}
	assertOfficialFixtureCount(t, ctx, database, detail.ID, detail.Versions[0].ID, 5)

	bridge, err := scriptruntime.NewBridge(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := scriptevent.NewPreviewRunner(bridge)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := database.ListScriptTestFixtures(ctx, 0, detail.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	wantVoice := map[string]string{
		"Sequence 202 — weighted closed":  "SA1081A002",
		"Sequence 202 — alternate closed": "SA1081A003",
		"After 202 — weighted closed":     "SA1081A002",
		"After 202 — not open yet":        "SA1081A005",
		"After 202 — closed already":      "SA1081A006",
	}
	for _, fixture := range fixtures {
		var state scriptevent.PreviewFixture
		if err := json.Unmarshal(fixture.Fixture, &state); err != nil {
			t.Fatal(err)
		}
		result, err := runner.Preview(ctx, scriptevent.PreviewRequest{
			Program: detail.Versions[0].CompiledProgram, StartNode: fixture.StartNode,
			Lines: detail.Versions[0].CompilerLines, Fixture: state,
		})
		if err != nil {
			t.Fatalf("preview %q: %v", fixture.Name, err)
		}
		if result.Outcome != "complete" || len(result.Steps) != 3 ||
			result.Steps[0].Event.Name != "play_sequence" || result.Steps[2].Event.Name != "play_sequence" {
			t.Fatalf("preview %q=%#v", fixture.Name, result)
		}
		line := result.Steps[1].Line
		voice := wantVoice[fixture.Name]
		if line == nil || voice == "" || !containsMetadata(line.Metadata, "voice:"+voice) || line.Text == nil || !strings.HasPrefix(*line.Text, "Ryo: ") {
			t.Fatalf("preview %q line=%#v, want voice %s", fixture.Name, line, voice)
		}
	}

	account, err := database.CreateRegisteredAccount(ctx, "door51-runtime-"+suffix+"@example.test", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.DB().Exec(`DELETE FROM accounts WHERE id=$1`, account.ID) })
	character, err := database.CreateCharacter(ctx, account.ID, "Door"+suffix, "ryo", "D000", 0, 0, 0, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO character_story_progress
		(character_id,key,value) VALUES ($1,'native.story.sequence',203)`, character.ID); err != nil {
		t.Fatal(err)
	}
	engine, err := scriptevent.New(database, bridge)
	if err != nil {
		t.Fatal(err)
	}
	hour := 5.0
	yield, err := engine.Start(ctx, account.ID, character.ID, scriptcontent.TriggerSelector{
		Kind: "use", Area: "D000", Object: definition.Triggers[0].Object,
	}, scriptevent.WorldFacts{GameHour: &hour})
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, yield, "command", "play_sequence")
	yield, err = engine.Advance(ctx, account.ID, character.ID, yield.RunID, scriptruntime.Continue())
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, yield, "line", "")
	if yield.Line == nil || (!containsMetadata(yield.Line.Metadata, "voice:SA1081A002") && !containsMetadata(yield.Line.Metadata, "voice:SA1081A005")) {
		t.Fatalf("runtime selected an impossible door 51 line: %#v", yield.Line)
	}
	yield, err = engine.Advance(ctx, account.ID, character.ID, yield.RunID, scriptruntime.Continue())
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, yield, "command", "play_sequence")
	yield, err = engine.Advance(ctx, account.ID, character.ID, yield.RunID, scriptruntime.Continue())
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, yield, "complete", "")
}
