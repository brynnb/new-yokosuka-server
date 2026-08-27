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

func TestHatoTranslationImportsAndExecutesFromPostgres(t *testing.T) {
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
	definition, _ := Lookup("d000-hato")
	definition.Slug = "hato-runtime-test-" + suffix
	definition.Title = "Hato runtime test " + suffix
	definition.SourceLocator += "?integration=" + suffix
	definition.Triggers[0].Actor = "HATO_TEST_" + suffix
	detail, err := database.ImportOfficialYarnScript(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Versions) != 1 || detail.Versions[0].NativeSourceLocator != definition.SourceLocator ||
		detail.Versions[0].NativeSourceHash != definition.SourceHash ||
		len(detail.Versions[0].NativeDialogueRegions) != 1 ||
		detail.Versions[0].NativeDialogueRegions[0].RegionStartFileOffset != 0x7fa98 ||
		detail.Versions[0].NativeDialogueRegions[0].Ownership != "translated" {
		t.Fatalf("immutable native provenance=%#v", detail.Versions)
	}
	if _, err := database.DB().Exec(`UPDATE script_version_native_dialogue_regions
		SET evidence_locator='changed' WHERE version_id=$1`, detail.Versions[0].ID); err == nil {
		t.Fatal("published dialogue-region provenance was mutable")
	}
	t.Cleanup(func() {
		_, _ = database.DB().Exec(`UPDATE scripts SET current_published_version_id=NULL WHERE id=$1`, detail.ID)
		_, _ = database.DB().Exec(`DELETE FROM scripts WHERE id=$1`, detail.ID)
	})
	second, err := database.ImportOfficialYarnScript(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	if second.CurrentVersion == nil || second.CurrentVersion.Version != 1 || second.CurrentVersion.Status != "published" {
		t.Fatalf("idempotent import produced %#v", second.CurrentVersion)
	}
	assertOfficialFixtureCount(t, ctx, database, detail.ID, detail.Versions[0].ID, 6)

	account, err := database.CreateRegisteredAccount(ctx, "hato-runtime-"+suffix+"@example.test", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.DB().Exec(`DELETE FROM accounts WHERE id=$1`, account.ID) })
	character, err := database.CreateCharacter(ctx, account.ID, "Hato"+suffix, "ryo", "D000", 0, 0, 0, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := scriptruntime.NewBridge(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	assertHatoCameraSelectionQueriesOnce(t, ctx, bridge, detail.Versions[0])
	previewOfficialFixtures(t, ctx, database, bridge, detail.ID, detail.Versions[0], map[string]string{
		"Eligible — camera route 1": "complete", "Eligible — camera route 2": "complete", "Eligible — camera route 3": "complete",
		"Ineligible — already spoken": "declined", "Ineligible — outside hours": "declined", "Ineligible — outside authored bounds": "declined",
	})
	engine, err := scriptevent.New(database, bridge)
	if err != nil {
		t.Fatal(err)
	}
	hour := 12.0
	yield, err := engine.Start(ctx, account.ID, character.ID, scriptcontent.TriggerSelector{
		Kind: "talk", Area: "D000", Actor: definition.Triggers[0].Actor,
	}, scriptevent.WorldFacts{
		GameHour: &hour,
		ActorBounds: map[string]map[string]bool{
			"AKIR": {"d000.hato.spatial.5": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, yield, "command", "start_camera")
	assertOneOfArgument(t, yield, map[string]bool{
		"d000.hato.camera.2950": true,
		"d000.hato.camera.2952": true,
		"d000.hato.camera.2954": true,
	})

	for _, expected := range []struct{ eventType, name string }{
		{"command", "look_at_actor"},
		{"command", "play_player_motion"},
		{"line", ""},
		{"command", "play_player_motion"},
		{"command", "clear_actor_look"},
		{"command", "stop_camera"},
		{"complete", ""},
	} {
		yield, err = engine.Advance(ctx, account.ID, character.ID, yield.RunID, scriptruntime.Continue())
		if err != nil {
			t.Fatal(err)
		}
		assertYield(t, yield, expected.eventType, expected.name)
		if expected.eventType == "line" {
			if yield.Line == nil || yield.Line.Text == nil ||
				*yield.Line.Text != "Hato: Ain't got time for punk kids.[br/]Get out of here." {
				t.Fatalf("unexpected Hato line metadata: %#v", yield.Line)
			}
		}
	}
	if yield.State == nil || yield.State.Progress["native.d1.character.HATO.dialogue_state"] != 1 {
		t.Fatalf("completed state=%#v", yield.State)
	}
	var progress float64
	if err := database.DB().QueryRowContext(ctx, `SELECT value FROM character_story_progress
		WHERE character_id=$1 AND key='native.d1.character.HATO.dialogue_state'`, character.ID).Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if progress != 1 {
		t.Fatalf("committed Hato dialogue state=%v, want 1", progress)
	}
}

func assertHatoCameraSelectionQueriesOnce(t *testing.T, ctx context.Context, bridge *scriptruntime.Bridge, version store.ScriptVersion) {
	t.Helper()
	session, err := bridge.Start(scriptruntime.StartRequest{Program: version.CompiledProgram, StartNode: "Start"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var input *scriptruntime.Input
	randomQueries := 0
	for step := 0; step < 64; step++ {
		event, err := session.Exchange(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		input = nil
		switch event.Type {
		case "nodeStart", "nodeComplete":
			continue
		case "query":
			var value scriptruntime.Value
			switch event.Name {
			case "flag_set":
				value = scriptruntime.Value{Type: "bool", Value: "false"}
			case "game_hour":
				value = scriptruntime.Value{Type: "number", Value: "12"}
			case "actor_in_bounds":
				value = scriptruntime.Value{Type: "bool", Value: "true"}
			case "random_integer":
				randomQueries++
				value = scriptruntime.Value{Type: "number", Value: "3"}
			default:
				t.Fatalf("unexpected Hato query %q", event.Name)
			}
			next := scriptruntime.QueryResult(*event.QueryID, value)
			input = &next
		case "command", "line":
			next := scriptruntime.Continue()
			input = &next
		case "complete":
			if randomQueries != 1 {
				t.Fatalf("Hato camera declaration queried random_integer %d times", randomQueries)
			}
			return
		default:
			t.Fatalf("unexpected Hato event %q", event.Type)
		}
	}
	t.Fatal("Hato camera query trace did not complete")
}

func previewOfficialFixtures(t *testing.T, ctx context.Context, database *store.Store, bridge *scriptruntime.Bridge, scriptID int64, version store.ScriptVersion, outcomes map[string]string) {
	t.Helper()
	runner, err := scriptevent.NewPreviewRunner(bridge)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := database.ListScriptTestFixtures(ctx, 0, scriptID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		var state scriptevent.PreviewFixture
		if err := json.Unmarshal(fixture.Fixture, &state); err != nil {
			t.Fatal(err)
		}
		result, err := runner.Preview(ctx, scriptevent.PreviewRequest{
			Program: version.CompiledProgram, StartNode: fixture.StartNode,
			Lines: version.CompilerLines, Fixture: state,
		})
		if err != nil {
			t.Fatalf("preview fixture %q: %v", fixture.Name, err)
		}
		if want, found := outcomes[fixture.Name]; !found || result.Outcome != want {
			t.Fatalf("preview fixture %q outcome=%q, want %q (known=%v)", fixture.Name, result.Outcome, want, found)
		}
	}
	if len(fixtures) != len(outcomes) {
		t.Fatalf("previewed %d fixtures, want %d", len(fixtures), len(outcomes))
	}
}

func assertOfficialFixtureCount(t *testing.T, ctx context.Context, database *store.Store, scriptID, versionID int64, count int) {
	t.Helper()
	fixtures, err := database.ListScriptTestFixtures(ctx, 0, scriptID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != count {
		t.Fatalf("official fixtures=%#v, want %d", fixtures, count)
	}
	for _, fixture := range fixtures {
		if fixture.Origin != "official" || fixture.SourceVersionID != versionID || fixture.StartNode == "" || fixture.Revision != 1 {
			t.Fatalf("unexpected official fixture: %#v", fixture)
		}
	}
}

func assertYield(t *testing.T, yield scriptevent.Yield, eventType, name string) {
	t.Helper()
	if yield.RuntimeEvent.Type != eventType || yield.RuntimeEvent.Name != name {
		t.Fatalf("yield event=%#v, want type=%q name=%q", yield.RuntimeEvent, eventType, name)
	}
}

func assertOneOfArgument(t *testing.T, yield scriptevent.Yield, allowed map[string]bool) {
	t.Helper()
	arguments := yield.RuntimeEvent.Arguments
	if len(arguments) != 1 || arguments[0].Value == nil || !allowed[*arguments[0].Value] {
		t.Fatalf("yield arguments=%#v, want one of %#v", arguments, allowed)
	}
}
