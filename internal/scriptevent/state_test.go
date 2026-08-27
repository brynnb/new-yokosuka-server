package scriptevent

import (
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func TestEventStateIncrementProgressStagesExactDelta(t *testing.T) {
	state := eventState{CharacterScriptState: store.CharacterScriptState{
		Progress: map[string]float64{"native.counter": 2},
	}}
	event := scriptruntime.Event{
		Type: "command", Name: "increment_progress", Sequence: 7,
		Arguments: []scriptcontent.CompiledArgument{
			staticEventArgument("string", "native.counter"),
			staticEventArgument("number", "1.5"),
		},
	}
	if err := state.stage(event); err != nil {
		t.Fatal(err)
	}
	if state.Progress["native.counter"] != 3.5 || len(state.effects) != 1 {
		t.Fatalf("state=%#v effects=%#v", state.Progress, state.effects)
	}
	if got := *state.effects[0].Arguments[1].Value; got != "1.5" {
		t.Fatalf("staged delta=%q, want 1.5", got)
	}
}

func TestEventStateRandomIntegerUsesAuthoritativeSource(t *testing.T) {
	called := false
	state := eventState{randomInteger: func(minimum, maximum int64) (int64, error) {
		called = true
		if minimum != -3 || maximum != 3 {
			t.Fatalf("range=[%d,%d], want [-3,3]", minimum, maximum)
		}
		return 2, nil
	}}
	event := scriptruntime.Event{
		Type: "query", Name: "random_integer",
		Arguments: []scriptcontent.CompiledArgument{
			staticEventArgument("number", "-3"),
			staticEventArgument("number", "3"),
		},
	}
	value, err := state.resolveQuery(event)
	if err != nil {
		t.Fatal(err)
	}
	if !called || value.Type != "number" || value.Value != "2" {
		t.Fatalf("called=%v value=%#v", called, value)
	}
}

func TestEventStateGameDateComparisonUsesAuthoritativeCalendar(t *testing.T) {
	date := CalendarDate{Year: 1986, Month: 4, Day: 1}
	state := eventState{facts: WorldFacts{GameDate: &date}}
	event := scriptruntime.Event{
		Type: "query", Name: "game_date_on_or_after",
		Arguments: []scriptcontent.CompiledArgument{
			staticEventArgument("number", "4"),
			staticEventArgument("number", "1"),
		},
	}
	value, err := state.resolveQuery(event)
	if err != nil {
		t.Fatal(err)
	}
	if value.Type != "bool" || value.Value != "true" {
		t.Fatalf("value=%#v, want true", value)
	}

	state.facts.GameDate = nil
	if _, err := state.resolveQuery(event); err == nil {
		t.Fatal("missing authoritative game date must fail closed")
	}
	event.Arguments[1] = staticEventArgument("number", "31")
	if _, err := state.resolveQuery(event); err == nil {
		t.Fatal("impossible calendar date must be rejected")
	}
}

func TestEventIntegerRejectsValuesYarnCannotRepresentExactly(t *testing.T) {
	event := scriptruntime.Event{Name: "random_integer", Arguments: []scriptcontent.CompiledArgument{
		staticEventArgument("number", "9007199254740992"),
	}}
	if _, err := eventInteger(event, 0); err == nil {
		t.Fatal("expected unsafe integer to be rejected")
	}
}

func staticEventArgument(kind, value string) scriptcontent.CompiledArgument {
	return scriptcontent.CompiledArgument{Type: kind, IsStatic: true, Value: &value}
}
