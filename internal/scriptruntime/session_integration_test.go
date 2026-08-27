package scriptruntime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

func TestOfficialRuntimeBridgeYieldsAuthoritativeSteps(t *testing.T) {
	path := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if path == "" {
		t.Skip("NEW_YOKOSUKA_YARN_COMPILER is not set")
	}
	compiler, err := scriptcontent.NewProcessCompiler(path)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(context.Background(), "runtime.yarn", `title: Start
---
<<if flag_set("hato.met")>>
Ryo: We have met before.
<<else>>
Ryo: Nice to meet you.
<<endif>>
<<set_flag "hato.met">>
<<complete>>
===
`)
	if err != nil || !compiled.Valid {
		t.Fatalf("compile: valid=%v err=%v diagnostics=%#v", compiled.Valid, err, compiled.Diagnostics)
	}
	bridge, err := NewBridge(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := bridge.Start(StartRequest{Program: compiled.Program, StartNode: "Start"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	event := exchange(t, ctx, session, nil, "nodeStart")
	if event.Node != "Start" {
		t.Fatalf("node=%q", event.Node)
	}
	event = exchange(t, ctx, session, nil, "query")
	if event.Name != "flag_set" || event.QueryID == nil || len(event.Arguments) != 1 ||
		event.Arguments[0].Value == nil || *event.Arguments[0].Value != "hato.met" {
		t.Fatalf("query=%#v", event)
	}
	event = exchange(t, ctx, session, pointer(QueryResult(*event.QueryID, Value{Type: "bool", Value: "true"})), "line")
	if event.LineID == "" {
		t.Fatal("line had no ID")
	}
	event = exchange(t, ctx, session, pointer(Continue()), "command")
	if event.Name != "set_flag" {
		t.Fatalf("command=%#v", event)
	}
	event = exchange(t, ctx, session, pointer(Continue()), "command")
	if event.Name != "complete" {
		t.Fatalf("command=%#v", event)
	}
	exchange(t, ctx, session, pointer(Continue()), "nodeComplete")
	exchange(t, ctx, session, nil, "complete")
}

func TestOfficialRuntimeBridgeCancelsAtYield(t *testing.T) {
	path := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if path == "" {
		t.Skip("NEW_YOKOSUKA_YARN_COMPILER is not set")
	}
	compiler, _ := scriptcontent.NewProcessCompiler(path)
	compiled, err := compiler.Compile(context.Background(), "cancel.yarn", "title: Start\n---\nRyo: Wait.\n===\n")
	if err != nil || !compiled.Valid {
		t.Fatalf("compile: valid=%v err=%v", compiled.Valid, err)
	}
	bridge, _ := NewBridge(path)
	session, err := bridge.Start(StartRequest{Program: compiled.Program, StartNode: "Start"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exchange(t, ctx, session, nil, "nodeStart")
	exchange(t, ctx, session, nil, "line")
	exchange(t, ctx, session, pointer(Cancel()), "cancelled")
}

func exchange(t *testing.T, ctx context.Context, session *Session, input *Input, want string) Event {
	t.Helper()
	event, err := session.Exchange(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != want {
		t.Fatalf("event=%#v, want type %q", event, want)
	}
	return event
}

func pointer(input Input) *Input { return &input }
