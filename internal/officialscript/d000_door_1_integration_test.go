package officialscript

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func TestDoor1ImportsPreviewsAndRunsAuthoritatively(t *testing.T) {
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
	definition, _ := Lookup("d000-door-1-closed-check")
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
		t.Fatalf("door 1 provenance=%#v", detail.Versions)
	}
	region := detail.Versions[0].NativeDialogueRegions[0]
	if region.ExecutableTargetIndex != 531 || region.RegionStartFileOffset != 0x74874 || region.Ownership != "translated" {
		t.Fatalf("door 1 region=%#v", region)
	}
	assertOfficialFixtureCount(t, ctx, database, detail.ID, detail.Versions[0].ID, 7)

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
		"Before open-hours flag — not open":  "SA1080A003",
		"Before open-hours flag — closed":    "SA1080A002",
		"Before 10:00 — closed":              "SA1080A004",
		"Before 10:00 — not open yet":        "SA1080A005",
		"At or after 21:00 — closed":         "SA1080A004",
		"At or after 21:00 — closed already": "SA1080A006",
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
		if result.Outcome != "complete" || len(result.Steps) < 2 ||
			result.Steps[0].Event.Name != "play_sequence" ||
			result.Steps[len(result.Steps)-1].Event.Name != "play_sequence" {
			t.Fatalf("preview %q=%#v", fixture.Name, result)
		}
		voice := wantVoice[fixture.Name]
		if fixture.Name == "Open hours — silent check" {
			if len(result.Steps) != 2 {
				t.Fatalf("open-hours route yielded dialogue: %#v", result.Steps)
			}
			continue
		}
		if len(result.Steps) != 3 || result.Steps[1].Line == nil ||
			!containsMetadata(result.Steps[1].Line.Metadata, "voice:"+voice) {
			t.Fatalf("preview %q line=%#v, want voice %s", fixture.Name, result.Steps, voice)
		}
	}

	account, err := database.CreateRegisteredAccount(ctx, "door1-runtime-"+suffix+"@example.test", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.DB().Exec(`DELETE FROM accounts WHERE id=$1`, account.ID) })
	character, err := database.CreateCharacter(ctx, account.ID, "Door"+suffix, "ryo", "D000", 0, 0, 0, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO character_story_flags
		(character_id,key,value) VALUES ($1,$2,true)`, character.ID, d000Door1StoryFlag); err != nil {
		t.Fatal(err)
	}
	engine, err := scriptevent.New(database, bridge)
	if err != nil {
		t.Fatal(err)
	}
	hour := 10.0
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
	assertYield(t, yield, "command", "play_sequence")
	yield, err = engine.Advance(ctx, account.ID, character.ID, yield.RunID, scriptruntime.Continue())
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, yield, "complete", "")
}
