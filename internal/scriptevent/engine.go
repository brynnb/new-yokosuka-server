package scriptevent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type Repository interface {
	StartPublishedScriptEventExcluding(context.Context, int64, int64, scriptcontent.TriggerSelector, []int64) (store.PublishedScriptEvent, error)
	RenewScriptEvent(context.Context, int64, string) (time.Time, error)
	CancelScriptEvent(context.Context, int64, string) error
	FailScriptEvent(context.Context, int64, string, string, string) error
	CompleteScriptEvent(context.Context, int64, string, int64, []store.ScriptEffect) (int64, error)
	PassScriptEvent(context.Context, int64, string, int64, []store.ScriptEffect) (int64, error)
	RecordScriptEventStep(context.Context, int64, string, store.ScriptEventTraceStep) error
}

type RuntimeSession interface {
	Exchange(context.Context, *scriptruntime.Input) (scriptruntime.Event, error)
	Close() error
}

type RuntimeStarter func(scriptruntime.StartRequest) (RuntimeSession, error)

type Yield struct {
	RunID        int64                           `json:"runId"`
	ScriptID     int64                           `json:"scriptId"`
	VersionID    int64                           `json:"versionId"`
	ScriptSlug   string                          `json:"scriptSlug"`
	RuntimeEvent scriptruntime.Event             `json:"event"`
	Line         *scriptcontent.CompiledLine     `json:"line,omitempty"`
	Options      []scriptcontent.PresentedOption `json:"options,omitempty"`
	State        *store.CharacterScriptState     `json:"state,omitempty"`
}

type activeEvent struct {
	mu                  sync.Mutex
	record              store.PublishedScriptEvent
	session             RuntimeSession
	state               eventState
	completionRequested bool
	visibleYielded      bool
	excludedVersionIDs  []int64
	lines               map[string]scriptcontent.CompiledLine
	traceOrdinal        int
	lastRuntimeSequence int
}

type Engine struct {
	repository    Repository
	start         RuntimeStarter
	randomInteger func(int64, int64) (int64, error)
	mu            sync.Mutex
	active        map[int64]*activeEvent
}

func New(repository Repository, bridge *scriptruntime.Bridge) (*Engine, error) {
	if bridge == nil {
		return nil, errors.New("Yarn runtime bridge is required")
	}
	return newEngine(repository, func(request scriptruntime.StartRequest) (RuntimeSession, error) {
		return bridge.Start(request)
	})
}

func newEngine(repository Repository, start RuntimeStarter) (*Engine, error) {
	if repository == nil || start == nil {
		return nil, errors.New("script event repository and runtime starter are required")
	}
	registry, err := scriptcontent.Registry()
	if err != nil {
		return nil, err
	}
	if err := validateReviewedBoundsCapabilities(registry); err != nil {
		return nil, err
	}
	for _, entry := range registry.Entries {
		if entry.Kind == "function" && !queryNames[entry.Name] {
			return nil, fmt.Errorf("registry query %q has no authoritative resolver", entry.Name)
		}
		if entry.Kind == "command" && !durableCommands[entry.Name] &&
			!externalCommands[entry.Name] && entry.Name != "complete" && entry.Name != "pass_trigger" {
			return nil, fmt.Errorf("registry command %q has no runtime disposition", entry.Name)
		}
	}
	return &Engine{
		repository:    repository,
		start:         start,
		randomInteger: secureRandomInteger,
		active:        map[int64]*activeEvent{},
	}, nil
}

func (engine *Engine) Start(
	ctx context.Context,
	accountID, characterID int64,
	selector scriptcontent.TriggerSelector,
	facts WorldFacts,
) (Yield, error) {
	return engine.startCandidate(ctx, accountID, characterID, selector, facts, nil)
}

func (engine *Engine) startCandidate(
	ctx context.Context,
	accountID, characterID int64,
	selector scriptcontent.TriggerSelector,
	facts WorldFacts,
	excludedVersionIDs []int64,
) (Yield, error) {
	record, err := engine.repository.StartPublishedScriptEventExcluding(
		ctx, accountID, characterID, selector, excludedVersionIDs,
	)
	if err != nil {
		return Yield{}, err
	}
	session, err := engine.start(scriptruntime.StartRequest{Program: record.Program, StartNode: record.EntryNode})
	if err != nil {
		_ = engine.repository.FailScriptEvent(ctx, record.RunID, record.LeaseToken, "runtime_start", err.Error())
		return Yield{}, err
	}
	active := &activeEvent{
		record: record, session: session,
		state: eventState{
			CharacterScriptState: cloneState(record.State),
			facts:                cloneFacts(facts),
			randomInteger:        engine.randomInteger,
		},
		excludedVersionIDs: append([]int64(nil), excludedVersionIDs...),
		lines:              make(map[string]scriptcontent.CompiledLine, len(record.Lines)),
	}
	for _, line := range record.Lines {
		active.lines[line.ID] = line
	}
	engine.mu.Lock()
	engine.active[record.RunID] = active
	engine.mu.Unlock()
	active.mu.Lock()
	defer active.mu.Unlock()
	return engine.pump(ctx, active, nil)
}

func (engine *Engine) Advance(
	ctx context.Context,
	accountID, characterID, runID int64,
	input scriptruntime.Input,
) (Yield, error) {
	engine.mu.Lock()
	active := engine.active[runID]
	engine.mu.Unlock()
	if active == nil || active.record.CharacterID != characterID || active.record.AccountID != accountID {
		return Yield{}, store.ErrNotFound
	}
	active.mu.Lock()
	defer active.mu.Unlock()
	if _, err := engine.repository.RenewScriptEvent(ctx, runID, active.record.LeaseToken); err != nil {
		engine.remove(active)
		_ = active.session.Close()
		return Yield{}, err
	}
	return engine.pump(ctx, active, &input)
}

func (engine *Engine) pump(ctx context.Context, active *activeEvent, input *scriptruntime.Input) (Yield, error) {
	for {
		if input != nil {
			if err := engine.recordTrace(ctx, active, active.lastRuntimeSequence, "controller", input.Type, input); err != nil {
				return Yield{}, engine.fail(ctx, active, "trace", err)
			}
		}
		event, err := active.session.Exchange(ctx, input)
		input = nil
		if err != nil {
			return Yield{}, engine.fail(ctx, active, "runtime", err)
		}
		if err := engine.recordTrace(ctx, active, event.Sequence, "runtime", event.Type, event); err != nil {
			return Yield{}, engine.fail(ctx, active, "trace", err)
		}
		active.lastRuntimeSequence = event.Sequence
		switch event.Type {
		case "nodeStart", "nodeComplete":
			continue
		case "query":
			value, err := active.state.resolveQuery(event)
			if err != nil {
				return Yield{}, engine.fail(ctx, active, "query", err)
			}
			inputValue := scriptruntime.QueryResult(*event.QueryID, value)
			input = &inputValue
		case "command":
			switch {
			case durableCommands[event.Name]:
				if err := active.state.stage(event); err != nil {
					return Yield{}, engine.fail(ctx, active, "effect", err)
				}
				continued := scriptruntime.Continue()
				input = &continued
			case event.Name == "complete":
				active.completionRequested = true
				continued := scriptruntime.Continue()
				input = &continued
			case event.Name == "pass_trigger":
				if active.visibleYielded {
					return Yield{}, engine.fail(ctx, active, "late_trigger_pass", errors.New("trigger candidates may only pass before a player-visible yield"))
				}
				newRevision, err := engine.repository.PassScriptEvent(
					ctx, active.record.RunID, active.record.LeaseToken,
					active.record.State.Revision, active.state.effects,
				)
				if err != nil {
					return Yield{}, engine.fail(ctx, active, "pass", err)
				}
				active.state.Revision = newRevision
				engine.remove(active)
				_ = active.session.Close()
				excluded := append(append([]int64(nil), active.excludedVersionIDs...), active.record.VersionID)
				next, err := engine.startCandidate(
					ctx, active.record.AccountID, active.record.CharacterID,
					active.record.Trigger, active.state.facts, excluded,
				)
				if errors.Is(err, store.ErrNotFound) {
					yield := active.yield(scriptruntime.Event{
						Type: "declined", Sequence: event.Sequence, Name: "pass_trigger",
					})
					state := cloneState(active.state.CharacterScriptState)
					yield.State = &state
					return yield, nil
				}
				return next, err
			case externalCommands[event.Name]:
				active.visibleYielded = true
				return active.yield(event), nil
			default:
				return Yield{}, engine.fail(ctx, active, "command", fmt.Errorf("command %q has no runtime disposition", event.Name))
			}
		case "line":
			if _, found := active.lines[event.LineID]; !found {
				return Yield{}, engine.fail(ctx, active, "line", fmt.Errorf("compiled line %q is unavailable", event.LineID))
			}
			active.visibleYielded = true
			return active.yield(event), nil
		case "options":
			options, err := presentOptions(active.lines, event.Options)
			if err != nil {
				return Yield{}, engine.fail(ctx, active, "options", err)
			}
			active.visibleYielded = true
			yield := active.yield(event)
			yield.Options = options
			return yield, nil
		case "cancelled":
			if err := engine.repository.CancelScriptEvent(ctx, active.record.RunID, active.record.LeaseToken); err != nil {
				return Yield{}, engine.fail(ctx, active, "cancel", err)
			}
			engine.remove(active)
			_ = active.session.Close()
			return active.yield(event), nil
		case "complete":
			if !active.completionRequested {
				return Yield{}, engine.fail(ctx, active, "completion_required", errors.New("script ended without the explicit complete command"))
			}
			newRevision, err := engine.repository.CompleteScriptEvent(
				ctx, active.record.RunID, active.record.LeaseToken,
				active.record.State.Revision, active.state.effects,
			)
			if err != nil {
				return Yield{}, engine.fail(ctx, active, "commit", err)
			}
			active.state.Revision = newRevision
			engine.remove(active)
			_ = active.session.Close()
			yield := active.yield(event)
			state := cloneState(active.state.CharacterScriptState)
			yield.State = &state
			return yield, nil
		default:
			return Yield{}, engine.fail(ctx, active, "protocol", fmt.Errorf("unexpected runtime event %q", event.Type))
		}
	}
}

func (engine *Engine) recordTrace(
	ctx context.Context,
	active *activeEvent,
	runtimeSequence int,
	direction, kind string,
	payload any,
) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode script event trace: %w", err)
	}
	step := store.ScriptEventTraceStep{
		Ordinal: active.traceOrdinal + 1, RuntimeSequence: runtimeSequence,
		Direction: direction, Kind: kind, Payload: encoded,
	}
	if err := engine.repository.RecordScriptEventStep(
		ctx, active.record.RunID, active.record.LeaseToken, step,
	); err != nil {
		return err
	}
	active.traceOrdinal = step.Ordinal
	return nil
}

func presentOptions(lines map[string]scriptcontent.CompiledLine, options []scriptruntime.Option) ([]scriptcontent.PresentedOption, error) {
	presented := make([]scriptcontent.PresentedOption, 0, len(options))
	seen := make(map[int]bool, len(options))
	available := false
	for _, option := range options {
		if seen[option.ID] {
			return nil, fmt.Errorf("Yarn runtime emitted duplicate option ID %d", option.ID)
		}
		seen[option.ID] = true
		line, found := lines[option.LineID]
		if !found || line.Text == nil {
			return nil, fmt.Errorf("compiled option line %q is unavailable", option.LineID)
		}
		available = available || option.IsAvailable
		presented = append(presented, scriptcontent.PresentedOption{
			ID: option.ID, IsAvailable: option.IsAvailable,
			Substitutions: append([]string(nil), option.Substitutions...),
			Line:          line,
		})
	}
	if !available {
		return nil, errors.New("Yarn runtime emitted no available options")
	}
	return presented, nil
}

func (active *activeEvent) yield(event scriptruntime.Event) Yield {
	yield := Yield{
		RunID: active.record.RunID, ScriptID: active.record.ScriptID,
		VersionID: active.record.VersionID, ScriptSlug: active.record.ScriptSlug,
		RuntimeEvent: event,
	}
	if event.Type == "line" {
		if line, found := active.lines[event.LineID]; found {
			lineCopy := line
			yield.Line = &lineCopy
		}
	}
	return yield
}

func (engine *Engine) fail(ctx context.Context, active *activeEvent, code string, cause error) error {
	engine.remove(active)
	_ = active.session.Close()
	if err := engine.repository.FailScriptEvent(ctx, active.record.RunID, active.record.LeaseToken, code, cause.Error()); err != nil && !errors.Is(err, store.ErrScriptEventEnded) {
		return fmt.Errorf("%v; record script failure: %w", cause, err)
	}
	return cause
}

func (engine *Engine) remove(active *activeEvent) {
	engine.mu.Lock()
	delete(engine.active, active.record.RunID)
	engine.mu.Unlock()
}

func cloneState(input store.CharacterScriptState) store.CharacterScriptState {
	result := input
	result.Flags = make(map[string]bool, len(input.Flags))
	for key, value := range input.Flags {
		result.Flags[key] = value
	}
	result.Progress = make(map[string]float64, len(input.Progress))
	for key, value := range input.Progress {
		result.Progress[key] = value
	}
	result.Inventory = make(map[string]int, len(input.Inventory))
	for key, value := range input.Inventory {
		result.Inventory[key] = value
	}
	return result
}

func cloneFacts(input WorldFacts) WorldFacts {
	result := input
	result.ActorPresence = cloneMap(input.ActorPresence)
	result.ObjectExistence = cloneMap(input.ObjectExistence)
	result.ActivityResults = cloneMap(input.ActivityResults)
	result.ActorStates = cloneNestedMap(input.ActorStates)
	result.ActorBounds = cloneNestedMap(input.ActorBounds)
	return result
}

func cloneMap[K comparable, V any](input map[K]V) map[K]V {
	result := make(map[K]V, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneNestedMap[V any](input map[string]map[string]V) map[string]map[string]V {
	result := make(map[string]map[string]V, len(input))
	for key, values := range input {
		result[key] = cloneMap(values)
	}
	return result
}

var queryNames = map[string]bool{
	"flag_set": true, "progress_value": true, "has_item": true, "yen": true,
	"actor_present": true, "in_scene": true, "game_hour": true,
	"game_date_on_or_after": true,
	"random_integer":        true,
	"actor_in_bounds":       true,
}

var durableCommands = map[string]bool{
	"set_flag": true, "clear_flag": true, "set_progress": true, "increment_progress": true,
	"give_item": true, "remove_item": true, "grant_yen": true, "spend_yen": true,
}

var externalCommands = map[string]bool{
	"play_player_motion": true, "look_at_actor": true, "clear_actor_look": true,
	"start_camera": true, "stop_camera": true,
	"play_sequence": true, "start_activity": true,
}
