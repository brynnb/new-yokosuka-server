package scriptevent

import (
	"context"
	"errors"
	"fmt"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

const maxPreviewEvents = 512

type PreviewFixture struct {
	Scene            string                        `json:"scene"`
	Yen              int64                         `json:"yen"`
	Flags            map[string]bool               `json:"flags"`
	Progress         map[string]float64            `json:"progress"`
	Inventory        map[string]int                `json:"inventory"`
	GameHour         *float64                      `json:"gameHour"`
	GameDate         *string                       `json:"gameDate"`
	ActorPresence    map[string]bool               `json:"actorPresence"`
	ActorStates      map[string]map[string]float64 `json:"actorStates"`
	ActorBounds      map[string]map[string]bool    `json:"actorBounds"`
	ObjectExistence  map[string]bool               `json:"objectExistence"`
	ActivityResults  map[string]string             `json:"activityResults"`
	RandomIntegers   []int64                       `json:"randomIntegers"`
	OptionSelections []int                         `json:"optionSelections"`
}

type PreviewRequest struct {
	Program   []byte
	StartNode string
	Lines     []scriptcontent.CompiledLine
	Fixture   PreviewFixture
}

type PreviewStep struct {
	Event   scriptruntime.Event             `json:"event"`
	Line    *scriptcontent.CompiledLine     `json:"line,omitempty"`
	Options []scriptcontent.PresentedOption `json:"options,omitempty"`
}

type PreviewResult struct {
	Outcome string                     `json:"outcome"`
	Steps   []PreviewStep              `json:"steps"`
	State   store.CharacterScriptState `json:"state"`
	Effects []store.ScriptEffect       `json:"effects"`
}

type PreviewRunner struct {
	start RuntimeStarter
}

func NewPreviewRunner(bridge *scriptruntime.Bridge) (*PreviewRunner, error) {
	if bridge == nil {
		return nil, errors.New("Yarn runtime bridge is required")
	}
	return &PreviewRunner{start: func(request scriptruntime.StartRequest) (RuntimeSession, error) {
		return bridge.Start(request)
	}}, nil
}

func (runner *PreviewRunner) Preview(ctx context.Context, request PreviewRequest) (PreviewResult, error) {
	if runner == nil || runner.start == nil || len(request.Program) == 0 || request.StartNode == "" {
		return PreviewResult{}, errors.New("compiled preview program and start node are required")
	}
	if err := ValidatePreviewFixture(request.Fixture); err != nil {
		return PreviewResult{}, err
	}
	session, err := runner.start(scriptruntime.StartRequest{
		Program: request.Program, StartNode: request.StartNode,
	})
	if err != nil {
		return PreviewResult{}, err
	}
	defer session.Close()
	fixture := request.Fixture
	var gameDate *CalendarDate
	if fixture.GameDate != nil {
		parsed, err := ParseCalendarDate(*fixture.GameDate)
		if err != nil {
			return PreviewResult{}, err
		}
		gameDate = &parsed
	}
	randomIndex, optionIndex := 0, 0
	state := eventState{
		CharacterScriptState: store.CharacterScriptState{
			Scene: fixture.Scene, Yen: fixture.Yen,
			Flags: cloneMap(fixture.Flags), Progress: cloneMap(fixture.Progress),
			Inventory: cloneMap(fixture.Inventory),
		},
		facts: WorldFacts{
			GameHour:        fixture.GameHour,
			GameDate:        gameDate,
			ActorPresence:   cloneMap(fixture.ActorPresence),
			ActorStates:     cloneNestedMap(fixture.ActorStates),
			ActorBounds:     cloneNestedMap(fixture.ActorBounds),
			ObjectExistence: cloneMap(fixture.ObjectExistence),
			ActivityResults: cloneMap(fixture.ActivityResults),
		},
		randomInteger: func(minimum, maximum int64) (int64, error) {
			if randomIndex >= len(fixture.RandomIntegers) {
				return 0, errors.New("preview fixture has no remaining random integer")
			}
			value := fixture.RandomIntegers[randomIndex]
			randomIndex++
			if value < minimum || value > maximum {
				return 0, fmt.Errorf("preview random integer %d is outside %d..%d", value, minimum, maximum)
			}
			return value, nil
		},
	}
	if state.Flags == nil {
		state.Flags = map[string]bool{}
	}
	if state.Progress == nil {
		state.Progress = map[string]float64{}
	}
	if state.Inventory == nil {
		state.Inventory = map[string]int{}
	}
	lines := make(map[string]scriptcontent.CompiledLine, len(request.Lines))
	for _, line := range request.Lines {
		lines[line.ID] = line
	}
	result := PreviewResult{Steps: []PreviewStep{}}
	var input *scriptruntime.Input
	completionRequested := false
	for count := 0; count < maxPreviewEvents; count++ {
		event, err := session.Exchange(ctx, input)
		input = nil
		if err != nil {
			return PreviewResult{}, err
		}
		switch event.Type {
		case "nodeStart", "nodeComplete":
			continue
		case "query":
			value, err := state.resolveQuery(event)
			if err != nil {
				return PreviewResult{}, err
			}
			next := scriptruntime.QueryResult(*event.QueryID, value)
			input = &next
		case "command":
			switch {
			case durableCommands[event.Name]:
				if err := state.stage(event); err != nil {
					return PreviewResult{}, err
				}
			case event.Name == "complete":
				completionRequested = true
			case event.Name == "pass_trigger":
				result.Steps = append(result.Steps, PreviewStep{Event: event})
				result.Outcome = "declined"
				result.State = cloneState(state.CharacterScriptState)
				result.Effects = append([]store.ScriptEffect(nil), state.effects...)
				return result, nil
			case externalCommands[event.Name]:
				result.Steps = append(result.Steps, PreviewStep{Event: event})
			default:
				return PreviewResult{}, fmt.Errorf("command %q has no preview disposition", event.Name)
			}
			next := scriptruntime.Continue()
			input = &next
		case "line":
			line, found := lines[event.LineID]
			if !found {
				return PreviewResult{}, fmt.Errorf("compiled line %q is unavailable", event.LineID)
			}
			lineCopy := line
			result.Steps = append(result.Steps, PreviewStep{Event: event, Line: &lineCopy})
			next := scriptruntime.Continue()
			input = &next
		case "options":
			presented, err := presentOptions(lines, event.Options)
			if err != nil {
				return PreviewResult{}, err
			}
			result.Steps = append(result.Steps, PreviewStep{
				Event: event, Options: presented,
			})
			if optionIndex >= len(fixture.OptionSelections) {
				return PreviewResult{}, errors.New("preview fixture has no remaining option selection")
			}
			selection := fixture.OptionSelections[optionIndex]
			optionIndex++
			next := scriptruntime.Select(selection)
			input = &next
		case "complete":
			if !completionRequested {
				return PreviewResult{}, errors.New("preview ended without explicit complete command")
			}
			result.Outcome = "complete"
			result.State = cloneState(state.CharacterScriptState)
			result.Effects = append([]store.ScriptEffect(nil), state.effects...)
			return result, nil
		case "cancelled":
			result.Outcome = "cancelled"
			result.State = cloneState(state.CharacterScriptState)
			return result, nil
		default:
			return PreviewResult{}, fmt.Errorf("unexpected preview event %q", event.Type)
		}
	}
	return PreviewResult{}, fmt.Errorf("preview exceeded %d runtime events", maxPreviewEvents)
}
