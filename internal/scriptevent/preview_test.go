package scriptevent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
)

type previewSession struct {
	events []scriptruntime.Event
	index  int
	inputs []*scriptruntime.Input
}

func (session *previewSession) Exchange(_ context.Context, input *scriptruntime.Input) (scriptruntime.Event, error) {
	session.inputs = append(session.inputs, input)
	event := session.events[session.index]
	session.index++
	return event, nil
}

func (*previewSession) Close() error { return nil }

func staticString(value string) scriptcontent.CompiledArgument {
	return scriptcontent.CompiledArgument{Type: "string", IsStatic: true, Value: &value}
}

func TestPreviewRunsWithoutCommittingAndRetainsPresentationTrace(t *testing.T) {
	queryID := 7
	session := &previewSession{events: []scriptruntime.Event{
		{Type: "nodeStart", Sequence: 1, Node: "Start"},
		{Type: "query", Sequence: 2, QueryID: &queryID, Name: "flag_set", Arguments: []scriptcontent.CompiledArgument{staticString("story.ready")}},
		{Type: "command", Sequence: 3, Name: "start_camera", Arguments: []scriptcontent.CompiledArgument{staticString("camera.one")}},
		{Type: "line", Sequence: 4, LineID: "line:hello"},
		{Type: "command", Sequence: 5, Name: "set_flag", Arguments: []scriptcontent.CompiledArgument{staticString("story.done")}},
		{Type: "command", Sequence: 6, Name: "complete"},
		{Type: "complete", Sequence: 7},
	}}
	runner := &PreviewRunner{start: func(scriptruntime.StartRequest) (RuntimeSession, error) {
		return session, nil
	}}
	lineText := "Ryo: Hello."
	result, err := runner.Preview(context.Background(), PreviewRequest{
		Program: []byte{1}, StartNode: "Start",
		Lines:   []scriptcontent.CompiledLine{{ID: "line:hello", Text: &lineText}},
		Fixture: PreviewFixture{Flags: map[string]bool{"story.ready": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "complete" || len(result.Steps) != 2 {
		t.Fatalf("preview result=%#v", result)
	}
	if result.Steps[0].Event.Name != "start_camera" || result.Steps[1].Line == nil || *result.Steps[1].Line.Text != lineText {
		t.Fatalf("preview trace=%#v", result.Steps)
	}
	if !result.State.Flags["story.done"] || len(result.Effects) != 1 {
		t.Fatalf("staged preview state=%#v effects=%#v", result.State, result.Effects)
	}
	if session.inputs[2] == nil || session.inputs[2].Type != "queryResult" || session.inputs[3] == nil || session.inputs[3].Type != "continue" {
		t.Fatalf("runtime inputs=%#v", session.inputs)
	}
}

func TestPreviewFailsClosedWithoutAuthoredRandomFixture(t *testing.T) {
	queryID := 1
	session := &previewSession{events: []scriptruntime.Event{{
		Type: "query", Sequence: 1, QueryID: &queryID, Name: "random_integer",
		Arguments: []scriptcontent.CompiledArgument{
			{Type: "number", IsStatic: true, Value: stringPointer("1")},
			{Type: "number", IsStatic: true, Value: stringPointer("3")},
		},
	}}}
	runner := &PreviewRunner{start: func(scriptruntime.StartRequest) (RuntimeSession, error) { return session, nil }}
	_, err := runner.Preview(context.Background(), PreviewRequest{Program: []byte{1}, StartNode: "Start"})
	if err == nil || !strings.Contains(err.Error(), "preview fixture has no remaining random integer") {
		t.Fatalf("preview error=%v", err)
	}
}

func stringPointer(value string) *string { return &value }

func TestPreviewRunsOfficialCompiledYarnWithoutPersistence(t *testing.T) {
	compilerPath := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if compilerPath == "" {
		t.Skip("official Yarn compiler is required")
	}
	compiler, err := scriptcontent.NewProcessCompiler(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(context.Background(), "preview.yarn", `title: Start
---
<<if flag_set("story.ready")>>
Ryo: Ready. #voice:TEST001 #speaker:AKIR
<<set_flag "story.previewed">>
<<endif>>
<<complete>>
===
`)
	if err != nil || !compiled.Valid {
		t.Fatalf("compile err=%v diagnostics=%#v", err, compiled.Diagnostics)
	}
	bridge, err := scriptruntime.NewBridge(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewPreviewRunner(bridge)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Preview(context.Background(), PreviewRequest{
		Program: compiled.Program, StartNode: "Start", Lines: compiled.Lines,
		Fixture: PreviewFixture{Flags: map[string]bool{"story.ready": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "complete" || len(result.Steps) != 1 ||
		result.Steps[0].Line == nil || !result.State.Flags["story.previewed"] {
		t.Fatalf("official preview=%#v", result)
	}
}
