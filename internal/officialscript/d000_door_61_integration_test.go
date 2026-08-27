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

func TestDoor61ImportsAndPreviewsEveryNativeBranch(t *testing.T) {
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
	definition, _ := Lookup("d000-door-61-closed-check")
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
	if len(detail.Versions) != 1 || len(detail.Versions[0].NativeDialogueRegions) != 1 {
		t.Fatalf("door 61 provenance=%#v", detail.Versions)
	}
	region := detail.Versions[0].NativeDialogueRegions[0]
	if region.ExecutableTargetIndex != 532 || region.RegionStartFileOffset != 0x7557c || region.Ownership != "translated" {
		t.Fatalf("door 61 region=%#v", region)
	}
	assertOfficialFixtureCount(t, ctx, database, detail.ID, detail.Versions[0].ID, 9)

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
		"April or later — can't get in":           "SA1088A001",
		"April or later — closed":                 "SA1088A002",
		"April or later — locked":                 "SA1088A003",
		"Before April after 05:00 — closed":       "SA1088A002",
		"Before April after 05:00 — not open yet": "SA1088A004",
		"Before April after 05:00 — locked":       "SA1088A003",
		"Before 05:00 — not open now":             "SA1088A005",
		"Before 05:00 — closed":                   "SA1088A002",
		"Before 05:00 — locked":                   "SA1088A003",
	}
	for _, fixture := range fixtures {
		var state scriptevent.PreviewFixture
		if err := json.Unmarshal(fixture.Fixture, &state); err != nil {
			t.Fatal(err)
		}
		result, err := runner.Preview(ctx, scriptevent.PreviewRequest{
			Program:   detail.Versions[0].CompiledProgram,
			StartNode: fixture.StartNode,
			Lines:     detail.Versions[0].CompilerLines,
			Fixture:   state,
		})
		if err != nil {
			t.Fatalf("preview %q: %v", fixture.Name, err)
		}
		if result.Outcome != "complete" || len(result.Steps) != 3 {
			t.Fatalf("preview %q=%#v", fixture.Name, result)
		}
		if result.Steps[0].Event.Name != "play_sequence" || result.Steps[2].Event.Name != "play_sequence" {
			t.Fatalf("preview %q sequence ownership=%#v", fixture.Name, result.Steps)
		}
		line := result.Steps[1].Line
		voice := wantVoice[fixture.Name]
		if line == nil || voice == "" || !containsMetadata(line.Metadata, "voice:"+voice) || line.Text == nil || !strings.HasPrefix(*line.Text, "Ryo: ") {
			t.Fatalf("preview %q line=%#v, want voice %s", fixture.Name, line, voice)
		}
	}
}

func containsMetadata(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
