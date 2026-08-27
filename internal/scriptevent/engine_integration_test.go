package scriptevent

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type memoryRepository struct {
	record    store.PublishedScriptEvent
	completed []store.ScriptEffect
	cancelled bool
	failed    string
	passed    []store.ScriptEffect
	trace     []store.ScriptEventTraceStep
}

func (repository *memoryRepository) RecordScriptEventStep(
	_ context.Context, _ int64, _ string, step store.ScriptEventTraceStep,
) error {
	repository.trace = append(repository.trace, step)
	return nil
}

func (repository *memoryRepository) StartPublishedScriptEventExcluding(
	_ context.Context, accountID, characterID int64, selector scriptcontent.TriggerSelector,
	excludedVersionIDs []int64,
) (store.PublishedScriptEvent, error) {
	for _, versionID := range excludedVersionIDs {
		if versionID == repository.record.VersionID {
			return store.PublishedScriptEvent{}, store.ErrNotFound
		}
	}
	record := repository.record
	record.AccountID, record.CharacterID, record.Trigger = accountID, characterID, selector
	return record, nil
}

func (repository *memoryRepository) PassScriptEvent(
	_ context.Context, _ int64, _ string, _ int64, effects []store.ScriptEffect,
) (int64, error) {
	repository.passed = append([]store.ScriptEffect(nil), effects...)
	return repository.record.State.Revision + 1, nil
}

func (repository *memoryRepository) RenewScriptEvent(context.Context, int64, string) (time.Time, error) {
	return time.Now().Add(time.Minute), nil
}

func (repository *memoryRepository) CancelScriptEvent(context.Context, int64, string) error {
	repository.cancelled = true
	return nil
}

func (repository *memoryRepository) FailScriptEvent(_ context.Context, _ int64, _, code, _ string) error {
	repository.failed = code
	return nil
}

func (repository *memoryRepository) CompleteScriptEvent(
	_ context.Context, _ int64, _ string, _ int64, effects []store.ScriptEffect,
) (int64, error) {
	repository.completed = append([]store.ScriptEffect(nil), effects...)
	return repository.record.State.Revision + 1, nil
}

func TestOfficialEngineStagesAndCommitsOnlyAfterComplete(t *testing.T) {
	path := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if path == "" {
		t.Skip("NEW_YOKOSUKA_YARN_COMPILER is not set")
	}
	compiler, err := scriptcontent.NewProcessCompiler(path)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(context.Background(), "engine.yarn", `title: Start
---
<<if flag_set("hato.met")>>
Ryo: Welcome back.
<<else>>
Ryo: Nice to meet you.
<<endif>>
<<set_flag "hato.met">>
<<set_progress "hato.stage" 2>>
<<if random_integer(1, 1) == 1>>
<<increment_progress "hato.stage" 1>>
<<endif>>
<<spend_yen 100 "telephone">>
<<start_camera "d000.hato.camera.2950">>
<<complete>>
===
`)
	if err != nil || !compiled.Valid {
		t.Fatalf("compile valid=%v err=%v diagnostics=%#v", compiled.Valid, err, compiled.Diagnostics)
	}
	repository := &memoryRepository{record: store.PublishedScriptEvent{
		RunID: 91, LeaseToken: "lease", ScriptID: 12, ScriptSlug: "hato", VersionID: 34,
		EntryNode: "Start", Program: compiled.Program, Lines: compiled.Lines,
		State: store.CharacterScriptState{
			Revision: 7, Scene: "D000", Yen: 500,
			Flags: map[string]bool{}, Progress: map[string]float64{}, Inventory: map[string]int{},
		},
	}}
	bridge, err := scriptruntime.NewBridge(path)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(repository, bridge)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	yield, err := engine.Start(ctx, 1, 2, scriptcontent.TriggerSelector{Kind: "talk", Area: "D000", Actor: "HATO"}, WorldFacts{})
	if err != nil || yield.RuntimeEvent.Type != "line" || yield.Line == nil {
		t.Fatalf("first yield=%#v err=%v", yield, err)
	}
	if len(repository.completed) != 0 {
		t.Fatal("effects committed before dialogue continued")
	}
	yield, err = engine.Advance(ctx, 1, 2, yield.RunID, scriptruntime.Continue())
	if err != nil || yield.RuntimeEvent.Type != "command" || yield.RuntimeEvent.Name != "start_camera" {
		t.Fatalf("presentation yield=%#v err=%v", yield, err)
	}
	if len(repository.completed) != 0 {
		t.Fatal("effects committed before presentation acknowledgement")
	}
	yield, err = engine.Advance(ctx, 1, 2, yield.RunID, scriptruntime.Continue())
	if err != nil || yield.RuntimeEvent.Type != "complete" {
		t.Fatalf("completion yield=%#v err=%v", yield, err)
	}
	if len(repository.completed) != 4 || repository.completed[0].Name != "set_flag" ||
		repository.completed[1].Name != "set_progress" || repository.completed[2].Name != "increment_progress" ||
		repository.completed[3].Name != "spend_yen" {
		t.Fatalf("committed effects=%#v", repository.completed)
	}
	if len(repository.trace) == 0 || repository.trace[0].Direction != "runtime" ||
		repository.trace[len(repository.trace)-1].Kind != "complete" {
		t.Fatalf("runtime trace=%#v", repository.trace)
	}
	queryResultRecorded := false
	for _, step := range repository.trace {
		if step.Direction == "controller" && step.Kind == "queryResult" {
			queryResultRecorded = true
		}
	}
	if !queryResultRecorded {
		t.Fatalf("authoritative query result missing from trace: %#v", repository.trace)
	}
}

func TestOfficialEngineCancellationRollsBackStagedEffects(t *testing.T) {
	path := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if path == "" {
		t.Skip("NEW_YOKOSUKA_YARN_COMPILER is not set")
	}
	compiler, _ := scriptcontent.NewProcessCompiler(path)
	compiled, err := compiler.Compile(context.Background(), "cancel-engine.yarn", `title: Start
---
<<set_flag "temporary">>
Ryo: Wait.
<<complete>>
===
`)
	if err != nil || !compiled.Valid {
		t.Fatalf("compile valid=%v err=%v", compiled.Valid, err)
	}
	repository := &memoryRepository{record: store.PublishedScriptEvent{
		RunID: 92, LeaseToken: "lease", ScriptID: 12, VersionID: 35,
		EntryNode: "Start", Program: compiled.Program, Lines: compiled.Lines,
		State: store.CharacterScriptState{Flags: map[string]bool{}, Progress: map[string]float64{}, Inventory: map[string]int{}},
	}}
	bridge, _ := scriptruntime.NewBridge(path)
	engine, _ := New(repository, bridge)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	yield, err := engine.Start(ctx, 1, 2, scriptcontent.TriggerSelector{Kind: "enter", Area: "D000"}, WorldFacts{})
	if err != nil || yield.RuntimeEvent.Type != "line" {
		t.Fatalf("yield=%#v err=%v", yield, err)
	}
	yield, err = engine.Advance(ctx, 1, 2, yield.RunID, scriptruntime.Cancel())
	if err != nil || yield.RuntimeEvent.Type != "cancelled" || !repository.cancelled {
		t.Fatalf("cancel yield=%#v cancelled=%v err=%v", yield, repository.cancelled, err)
	}
	if len(repository.completed) != 0 || repository.failed != "" {
		t.Fatalf("cancel committed=%#v failed=%q", repository.completed, repository.failed)
	}
	if _, err := engine.Advance(ctx, 1, 2, yield.RunID, scriptruntime.Continue()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("advance ended run err=%v, want ErrNotFound", err)
	}
}

func TestPresentedOptionsUseCompiledLinesAndPreserveAvailability(t *testing.T) {
	yes, no := "Ask about the harbor", "Leave"
	lines := map[string]scriptcontent.CompiledLine{
		"line:yes": {ID: "line:yes", Text: &yes},
		"line:no":  {ID: "line:no", Text: &no},
	}
	options, err := presentOptions(lines, []scriptruntime.Option{
		{ID: 3, LineID: "line:yes", Substitutions: []string{"harbor"}, IsAvailable: true},
		{ID: 7, LineID: "line:no", IsAvailable: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 || options[0].ID != 3 || options[0].Line.ID != "line:yes" || options[1].IsAvailable {
		t.Fatalf("presented options=%#v", options)
	}
	if _, err := presentOptions(lines, []scriptruntime.Option{
		{ID: 8, LineID: "missing", IsAvailable: true},
	}); err == nil {
		t.Fatal("missing compiled option line was accepted")
	}
}

func TestOfficialEnginePresentsAndAcceptsACompiledYarnChoice(t *testing.T) {
	path := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if path == "" {
		t.Skip("NEW_YOKOSUKA_YARN_COMPILER is not set")
	}
	compiler, err := scriptcontent.NewProcessCompiler(path)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(context.Background(), "choice-engine.yarn", `title: Start
---
-> Ask about the harbor
    Ryo: I'll ask. #speaker:AKIR
-> Leave
    Ryo: Not now. #speaker:AKIR
<<complete>>
===
`)
	if err != nil || !compiled.Valid {
		t.Fatalf("compile valid=%v err=%v diagnostics=%#v", compiled.Valid, err, compiled.Diagnostics)
	}
	repository := &memoryRepository{record: store.PublishedScriptEvent{
		RunID: 93, LeaseToken: "lease", ScriptID: 12, VersionID: 36,
		EntryNode: "Start", Program: compiled.Program, Lines: compiled.Lines,
		State: store.CharacterScriptState{
			Flags: map[string]bool{}, Progress: map[string]float64{}, Inventory: map[string]int{},
		},
	}}
	bridge, err := scriptruntime.NewBridge(path)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(repository, bridge)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	yield, err := engine.Start(ctx, 1, 2, scriptcontent.TriggerSelector{
		Kind: "talk", Area: "D000", Actor: "HATO",
	}, WorldFacts{})
	if err != nil || yield.RuntimeEvent.Type != "options" || len(yield.Options) != 2 {
		t.Fatalf("choice yield=%#v err=%v", yield, err)
	}
	selected := yield.Options[0]
	if !selected.IsAvailable || selected.Line.Text == nil || *selected.Line.Text != "Ask about the harbor" {
		t.Fatalf("first option=%#v", selected)
	}
	yield, err = engine.Advance(ctx, 1, 2, yield.RunID, scriptruntime.Select(selected.ID))
	if err != nil || yield.RuntimeEvent.Type != "line" || yield.Line == nil {
		t.Fatalf("selected yield=%#v err=%v", yield, err)
	}
	yield, err = engine.Advance(ctx, 1, 2, yield.RunID, scriptruntime.Continue())
	if err != nil || yield.RuntimeEvent.Type != "complete" {
		t.Fatalf("completion yield=%#v err=%v", yield, err)
	}
}
