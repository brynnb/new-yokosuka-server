package realtime

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
	"github.com/brynnb/new-yokosuka-server/internal/worldstate"
)

type fakeScriptEventEngine struct {
	starts   int
	selector scriptcontent.TriggerSelector
	facts    scriptevent.WorldFacts
	input    scriptruntime.Input
	start    scriptevent.Yield
	advance  scriptevent.Yield
}

func (engine *fakeScriptEventEngine) Start(
	_ context.Context, _, _ int64, selector scriptcontent.TriggerSelector, facts scriptevent.WorldFacts,
) (scriptevent.Yield, error) {
	engine.starts++
	engine.selector, engine.facts = selector, facts
	return engine.start, nil
}

func TestAutomaticScriptEventDispatchesOncePerWorldResidency(t *testing.T) {
	engine := &fakeScriptEventEngine{start: scriptevent.Yield{
		RunID: 41, ScriptID: 5, VersionID: 8, ScriptSlug: "automatic",
		RuntimeEvent: scriptruntime.Event{Type: "declined", Sequence: 2},
	}}
	hub, client := scriptEventTestHub(t, engine)
	for _, requestID := range []string{"automatic_1", "automatic_2"} {
		hub.HandleScriptEventStart(client, protocol.ScriptEventStartRequest{
			RequestID: requestID, Kind: "automatic",
		})
	}
	if engine.starts != 1 {
		t.Fatalf("automatic starts=%d, want 1", engine.starts)
	}
	<-client.send
	var duplicate protocol.ScriptEventRejected
	if err := json.Unmarshal(<-client.send, &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.Code != "automatic_already_dispatched" {
		t.Fatalf("duplicate response=%#v", duplicate)
	}

	client.markLocation(store.Location{WorldID: "exterior"})
	client.markLocation(store.Location{WorldID: "dobuita"})
	hub.HandleScriptEventStart(client, protocol.ScriptEventStartRequest{
		RequestID: "automatic_3", Kind: "automatic",
	})
	if engine.starts != 2 {
		t.Fatalf("automatic starts after re-entry=%d, want 2", engine.starts)
	}
}

func (engine *fakeScriptEventEngine) Advance(
	_ context.Context, _, _, _ int64, input scriptruntime.Input,
) (scriptevent.Yield, error) {
	engine.input = input
	return engine.advance, nil
}

func scriptEventTestHub(t *testing.T, engine ScriptEventEngine) (*Hub, *Client) {
	t.Helper()
	clock, err := worldstate.NewClock(time.Now(), "summer")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(5, worldstate.NewManager(clock), log.New(io.Discard, "", 0), nil)
	hub.SetScriptEngine(engine, map[string]string{"D000": "dobuita"})
	character := &store.Character{ID: 20, AccountID: 10, Name: "Ryo", WorldID: "dobuita"}
	client := newClient(hub, nil, "player", "Ryo", ConnectionMetadata{
		AccountID: 10, AccountType: "registered", Character: character,
	})
	hub.clients[client.id] = client
	return hub, client
}

func TestScriptEventStartDerivesNativeAreaAndYields(t *testing.T) {
	engine := &fakeScriptEventEngine{start: scriptevent.Yield{
		RunID: 41, ScriptID: 5, VersionID: 8, ScriptSlug: "hato",
		RuntimeEvent: scriptruntime.Event{Type: "line", Sequence: 2, LineID: "line:test"},
	}}
	hub, client := scriptEventTestHub(t, engine)
	if !hub.HandleScriptEventStart(client, protocol.ScriptEventStartRequest{
		RequestID: "request_123", Kind: "talk", Actor: "HATO",
	}) {
		t.Fatal("start response was not queued")
	}
	if engine.selector != (scriptcontent.TriggerSelector{Kind: "talk", Area: "D000", Actor: "HATO"}) {
		t.Fatalf("selector=%#v", engine.selector)
	}
	if engine.facts.GameHour == nil || engine.facts.GameDate == nil || client.scriptRun() != 41 {
		t.Fatalf("facts=%#v activeRun=%d", engine.facts, client.scriptRun())
	}
	var response protocol.ScriptEventYield
	if err := json.Unmarshal(<-client.send, &response); err != nil {
		t.Fatal(err)
	}
	if response.Header.Type != protocol.TypeScriptEventYield || response.RunID != 41 || response.Event.Type != "line" {
		t.Fatalf("response=%#v", response)
	}
}

func TestScriptEventAdvanceClearsCompletedRun(t *testing.T) {
	state := &store.CharacterScriptState{Revision: 4, Yen: 500}
	engine := &fakeScriptEventEngine{advance: scriptevent.Yield{
		RunID: 41, ScriptID: 5, VersionID: 8, ScriptSlug: "hato", State: state,
		RuntimeEvent: scriptruntime.Event{Type: "complete", Sequence: 9},
	}}
	hub, client := scriptEventTestHub(t, engine)
	client.setScriptRun(41)
	if !hub.HandleScriptEventAdvance(client, protocol.ScriptEventAdvanceRequest{
		RequestID: "request_456", RunID: 41, Action: "continue",
	}) {
		t.Fatal("advance response was not queued")
	}
	if engine.input.Type != "continue" || client.scriptRun() != 0 {
		t.Fatalf("input=%#v activeRun=%d", engine.input, client.scriptRun())
	}
	var response protocol.ScriptEventYield
	if err := json.Unmarshal(<-client.send, &response); err != nil {
		t.Fatal(err)
	}
	if response.State == nil || response.State.Revision != 4 {
		t.Fatalf("response=%#v", response)
	}
}

func TestScriptEventOptionsCarryCompiledTextAndAcceptSelection(t *testing.T) {
	text := "Ask about the harbor"
	engine := &fakeScriptEventEngine{advance: scriptevent.Yield{
		RunID: 41, ScriptID: 5, VersionID: 8, ScriptSlug: "choice",
		RuntimeEvent: scriptruntime.Event{Type: "options", Sequence: 3},
		Options: []scriptcontent.PresentedOption{{
			ID: 2, IsAvailable: true,
			Line: scriptcontent.CompiledLine{ID: "line:choice", Text: &text},
		}},
	}}
	hub, client := scriptEventTestHub(t, engine)
	client.setScriptRun(41)
	optionID := 2
	hub.HandleScriptEventAdvance(client, protocol.ScriptEventAdvanceRequest{
		RequestID: "request_option", RunID: 41, Action: "select", OptionID: &optionID,
	})
	if engine.input.Type != "select" || engine.input.OptionID == nil || *engine.input.OptionID != 2 {
		t.Fatalf("input=%#v", engine.input)
	}
	var response protocol.ScriptEventYield
	if err := json.Unmarshal(<-client.send, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Options) != 1 || response.Options[0].Line.Text == nil || *response.Options[0].Line.Text != text {
		t.Fatalf("response options=%#v", response.Options)
	}
}

func TestScriptEventRejectsAmbiguousWorldArea(t *testing.T) {
	engine := &fakeScriptEventEngine{}
	hub, client := scriptEventTestHub(t, engine)
	hub.SetScriptEngine(engine, map[string]string{"D000": "dobuita", "D001": "dobuita"})
	hub.HandleScriptEventStart(client, protocol.ScriptEventStartRequest{
		RequestID: "request_789", Kind: "enter",
	})
	var response protocol.ScriptEventRejected
	if err := json.Unmarshal(<-client.send, &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "area_unavailable" {
		t.Fatalf("response=%#v", response)
	}
}
