package officialscript

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func TestGoroTelephoneImportsWithProvenanceAndExecutesAllVariants(t *testing.T) {
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
	definition, _ := Lookup("jomo-goro-telephone")
	definition.Slug = "telephone-runtime-test-" + suffix
	definition.Title = "Telephone runtime test " + suffix
	definition.NativeSources[0].Locator += "?integration=" + suffix
	definition.Triggers[0].Area = "JOMO_TEST_" + suffix
	detail, err := database.ImportOfficialYarnScript(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.DB().Exec(`UPDATE scripts SET current_published_version_id=NULL WHERE id=$1`, detail.ID)
		_, _ = database.DB().Exec(`DELETE FROM scripts WHERE id=$1`, detail.ID)
	})
	if len(detail.Versions) != 1 || len(detail.Versions[0].NativeSources) != 2 ||
		detail.Versions[0].NativeSources[0].Role != "room-program" ||
		detail.Versions[0].NativeSources[1].Role != "dialogue-archive" ||
		len(detail.Versions[0].NativeDialogueRegions) != 3 {
		t.Fatalf("telephone native provenance=%#v", detail.Versions)
	}
	for index, offset := range []int64{0x7c670, 0x7cdda, 0x7d45a} {
		region := detail.Versions[0].NativeDialogueRegions[index]
		if region.RegionStartFileOffset != offset || region.Ownership != "translated" {
			t.Fatalf("telephone dialogue region %d=%#v", index, region)
		}
	}
	reimported, err := database.ImportOfficialYarnScript(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	if reimported.CurrentVersion == nil || reimported.CurrentVersion.Version != 1 {
		t.Fatalf("multi-source import was not idempotent: %#v", reimported.CurrentVersion)
	}
	assertOfficialFixtureCount(t, ctx, database, detail.ID, detail.Versions[0].ID, 5)
	if _, err := database.DB().Exec(`UPDATE script_version_native_sources SET role='changed' WHERE version_id=$1 AND ordinal=1`, detail.Versions[0].ID); err == nil {
		t.Fatal("published native provenance was mutable")
	}
	fallback, err := database.ImportOfficialYarnScript(ctx, store.OfficialYarnImport{
		Slug:          "telephone-fallback-test-" + suffix,
		Title:         "Telephone fallback test " + suffix,
		Description:   "Lower-priority automatic candidate used to verify trigger passing.",
		Summary:       "Yields only when the higher-priority telephone gate passes.",
		SourceLocator: "integration://telephone-fallback/" + suffix,
		SourceHash:    strings.Repeat("c", 64),
		SourceText: `title: Start
---
Ryo: Lower-priority automatic event.
<<complete>>
===
`,
		Triggers: []scriptcontent.Trigger{{
			NodeID: "Start", Kind: "automatic", Area: definition.Triggers[0].Area, Priority: 50,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.DB().Exec(`UPDATE scripts SET current_published_version_id=NULL WHERE id=$1`, fallback.ID)
		_, _ = database.DB().Exec(`DELETE FROM scripts WHERE id=$1`, fallback.ID)
	})

	account, err := database.CreateRegisteredAccount(ctx, "telephone-runtime-"+suffix+"@example.test", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.DB().Exec(`DELETE FROM accounts WHERE id=$1`, account.ID) })
	character, err := database.CreateCharacter(ctx, account.ID, "Phone"+suffix, "ryo", "JOMO", 0, 0, 0, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO character_script_state (character_id) VALUES ($1)`,
		`INSERT INTO character_story_progress (character_id,key,value) VALUES ($1,'native.story.disc',3)`,
		`INSERT INTO character_story_flags (character_id,key,value) VALUES ($1,'native.jomo.free_conversation.bank2.bit410',true)`,
	} {
		if _, err := database.DB().ExecContext(ctx, statement, character.ID); err != nil {
			t.Fatal(err)
		}
	}
	bridge, err := scriptruntime.NewBridge(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	previewOfficialFixtures(t, ctx, database, bridge, detail.ID, detail.Versions[0], map[string]string{
		"Arm incoming call": "declined", "First missed-work call": "complete",
		"Second missed-work call": "complete", "Repeat missed-work call": "complete",
		"Finished — pass trigger": "declined",
	})
	engine, err := scriptevent.New(database, bridge)
	if err != nil {
		t.Fatal(err)
	}
	selector := scriptcontent.TriggerSelector{Kind: "automatic", Area: definition.Triggers[0].Area}

	armed, err := engine.Start(ctx, account.ID, character.ID, selector, scriptevent.WorldFacts{})
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, armed, "line", "")
	if armed.ScriptSlug != fallback.Slug {
		t.Fatalf("passed telephone gate selected %q, want lower-priority %q", armed.ScriptSlug, fallback.Slug)
	}
	armed, err = engine.Advance(ctx, account.ID, character.ID, armed.RunID, scriptruntime.Continue())
	if err != nil {
		t.Fatal(err)
	}
	assertYield(t, armed, "complete", "")
	if armed.State == nil || !armed.State.Flags["native.jomo.free_conversation.bank2.bit415"] {
		t.Fatalf("telephone arming state=%#v", armed.State)
	}
	var passedRuns int
	if err := database.DB().QueryRowContext(ctx, `SELECT count(*) FROM script_event_runs
		WHERE character_id=$1 AND version_id=$2 AND status='passed'`,
		character.ID, detail.Versions[0].ID).Scan(&passedRuns); err != nil {
		t.Fatal(err)
	}
	if passedRuns != 1 {
		t.Fatalf("telephone passed run count=%d, want 1", passedRuns)
	}

	firstCommands, firstVoices := runTelephoneCall(t, ctx, engine, account.ID, character.ID, selector)
	assertTelephoneCommands(t, firstCommands, []string{
		"play_sequence:jomo.telephone.ring.sa1093",
		"start_camera:jomo.telephone.camera.3031",
		"play_sequence:jomo.telephone.answer.sa1093.first",
		"start_camera:jomo.telephone.camera.3032",
		"start_camera:jomo.telephone.camera.3033",
		"start_camera:jomo.telephone.camera.3034",
		"play_sequence:jomo.telephone.hangup.sa1093.first",
		"stop_camera:",
	})
	assertTelephoneVoices(t, firstVoices, []string{
		"SA1093A001", "SA1093B001", "SA1093A002", "SA1093B002", "SA1093B003",
		"SA1093A003", "SA1093B004", "SA1093B005", "SA1093B006", "SA1093A004", "SA1093B007",
	})

	secondCommands, secondVoices := runTelephoneCall(t, ctx, engine, account.ID, character.ID, selector)
	assertTelephoneCommands(t, secondCommands, []string{
		"play_sequence:jomo.telephone.ring.sa1093",
		"start_camera:jomo.telephone.camera.3031",
		"play_sequence:jomo.telephone.answer.sa1093.later",
		"start_camera:jomo.telephone.camera.3035",
		"start_camera:jomo.telephone.camera.3036",
		"start_camera:jomo.telephone.camera.3037",
		"play_sequence:jomo.telephone.hangup.sa1093.later",
		"stop_camera:",
	})
	assertTelephoneVoices(t, secondVoices, []string{
		"SA1093A001", "SA1093B001", "SA1093A005", "SA1093B008", "SA1093B009",
		"SA1093A006", "SA1093B006", "SA1093A004", "SA1093B007",
	})

	_, repeatVoices := runTelephoneCall(t, ctx, engine, account.ID, character.ID, selector)
	assertTelephoneVoices(t, repeatVoices, []string{
		"SA1093A001", "SA1093B001", "SA1093A005", "SA1093B008", "SA1093B011",
		"SA1093A007", "SA1093B010", "SA1093A004", "SA1093B012",
	})
}

func runTelephoneCall(
	t *testing.T,
	ctx context.Context,
	engine *scriptevent.Engine,
	accountID, characterID int64,
	selector scriptcontent.TriggerSelector,
) ([]string, []string) {
	t.Helper()
	yield, err := engine.Start(ctx, accountID, characterID, selector, scriptevent.WorldFacts{})
	if err != nil {
		t.Fatal(err)
	}
	commands, voices := []string{}, []string{}
	for step := 0; step < 64; step++ {
		switch yield.RuntimeEvent.Type {
		case "command":
			argument := ""
			if len(yield.RuntimeEvent.Arguments) > 0 && yield.RuntimeEvent.Arguments[0].Value != nil {
				argument = *yield.RuntimeEvent.Arguments[0].Value
			}
			commands = append(commands, yield.RuntimeEvent.Name+":"+argument)
		case "line":
			if yield.Line == nil {
				t.Fatal("telephone line omitted compiler metadata")
			}
			for _, metadata := range yield.Line.Metadata {
				if strings.HasPrefix(metadata, "voice:") {
					voices = append(voices, strings.TrimPrefix(metadata, "voice:"))
				}
			}
		case "complete":
			if yield.State == nil {
				t.Fatal("telephone completion omitted committed state")
			}
			return commands, voices
		}
		yield, err = engine.Advance(ctx, accountID, characterID, yield.RunID, scriptruntime.Continue())
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("telephone call exceeded 64 yielded events")
	return nil, nil
}

func assertTelephoneCommands(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("telephone commands=%#v, want %#v", got, want)
	}
}

func assertTelephoneVoices(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("telephone voices=%#v, want %#v", got, want)
	}
}
