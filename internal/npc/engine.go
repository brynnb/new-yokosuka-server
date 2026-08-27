package npc

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

const (
	clearHysteresis       = 500 * time.Millisecond
	blockedEncounterReset = 2 * time.Second
	npcAvoidancePatience  = 1 * time.Second
	correctionInterval    = 2 * time.Second
	avoidancePatience     = 15 * time.Second
)

type TickTime struct {
	ServerTime time.Time
	GameTime   time.Time
	DayNumber  int64
	DayLength  time.Duration
}

type runtimeState struct {
	dayNumber         int64
	scheduleVariantID string
	accumulatedDelay  float64
	blockedBy         string
	blockedSince      time.Time
	clearSince        time.Time
	avoidancePhase    AvoidancePhase
	avoidanceOffset   Vector3
	avoidanceTarget   Vector3
	avoidanceMotion   float64
	revision          uint64
	state             State
	lastCorrection    time.Time
}

type Engine struct {
	mu            sync.RWMutex
	actors        []npcdata.Actor
	actorByID     map[string]npcdata.Actor
	interpreter   *Interpreter
	runtimes      map[string]*runtimeState
	lastTick      TickTime
	started       bool
	selectorState ScheduleSelectorState
}

func NewEngine(manifest *npcdata.Manifest) (*Engine, error) {
	if manifest == nil || len(manifest.Actors) == 0 {
		return nil, fmt.Errorf("NPC manifest has no actors")
	}
	actors := append([]npcdata.Actor(nil), manifest.Actors...)
	sort.Slice(actors, func(left, right int) bool {
		return actors[left].InstanceID < actors[right].InstanceID
	})
	engine := &Engine{
		actors:      actors,
		actorByID:   make(map[string]npcdata.Actor, len(actors)),
		interpreter: NewInterpreter(manifest.AreaWorlds),
		runtimes:    make(map[string]*runtimeState, len(actors)),
		selectorState: ScheduleSelectorState{
			BaseSelector: 1,
			StoryFlags:   map[int]bool{},
		},
	}
	if err := engine.interpreter.Compile(actors); err != nil {
		return nil, fmt.Errorf("compile NPC programs: %w", err)
	}
	for _, actor := range actors {
		if actor.InstanceID == "" {
			return nil, fmt.Errorf("NPC manifest contains an empty actor ID")
		}
		if _, duplicate := engine.actorByID[actor.InstanceID]; duplicate {
			return nil, fmt.Errorf("duplicate NPC actor ID %s", actor.InstanceID)
		}
		engine.actorByID[actor.InstanceID] = actor
		engine.runtimes[actor.InstanceID] = &runtimeState{dayNumber: -1}
	}
	return engine, nil
}

func (e *Engine) Restore(ctx context.Context, store CheckpointStore) error {
	if store == nil {
		return nil
	}
	checkpoints, err := store.LoadNPCCheckpoints(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, checkpoint := range checkpoints {
		runtime := e.runtimes[checkpoint.NPCID]
		if runtime == nil {
			continue
		}
		runtime.dayNumber = checkpoint.DayNumber
		runtime.accumulatedDelay = math.Max(0, checkpoint.AccumulatedDelay)
		// Blockers are connection-scoped. A restored NPC resumes from its
		// checkpoint instead of remaining attached to a stale player ID.
		runtime.blockedBy = ""
		runtime.blockedSince = time.Time{}
		runtime.resetAvoidance()
		runtime.revision = checkpoint.Revision
	}
	return nil
}

// ResetScheduleState discards runtime delays and avoidance state while keeping
// each actor's last published state and revision. The next Tick therefore
// publishes removals and teleport transitions from the old time to the newly
// evaluated schedule time without allowing stale revisions to win on clients.
func (e *Engine) ResetScheduleState() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, runtime := range e.runtimes {
		runtime.dayNumber = -1
		runtime.scheduleVariantID = ""
		runtime.accumulatedDelay = 0
		runtime.blockedBy = ""
		runtime.blockedSince = time.Time{}
		runtime.clearSince = time.Time{}
		runtime.resetAvoidance()
		runtime.lastCorrection = time.Time{}
	}
	e.lastTick = TickTime{}
	e.started = false
}

func (e *Engine) Tick(clock TickTime, players []Player) ([]Change, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if clock.ServerTime.IsZero() || clock.GameTime.IsZero() {
		return nil, fmt.Errorf("NPC tick requires server and game times")
	}
	gameDelta := 0.0
	realDelta := 0.0
	if e.started && clock.DayNumber == e.lastTick.DayNumber {
		gameDelta = clock.GameTime.Sub(e.lastTick.GameTime).Seconds()
		if gameDelta < 0 || gameDelta > gameDaySeconds {
			gameDelta = 0
		}
		realDelta = clock.ServerTime.Sub(e.lastTick.ServerTime).Seconds()
		if realDelta < 0 || realDelta > gameDaySeconds {
			realDelta = 0
		}
	}
	realSecondsPerGameSecond := 1.0
	if clock.DayLength > 0 {
		realSecondsPerGameSecond = clock.DayLength.Seconds() /
			gameDaySeconds
	}

	states := make([]State, 0, len(e.actors))
	for _, actor := range e.actors {
		runtime := e.runtimes[actor.InstanceID]
		freezeSchedule := runtime.blockedBy != "" ||
			runtime.avoidancePhase == AvoidanceSidestep
		newDay := runtime.dayNumber != clock.DayNumber
		if newDay || runtime.scheduleVariantID == "" {
			variantID, err := SelectScheduleVariant(
				actor,
				clock.GameTime,
				e.selectorState,
			)
			if err != nil {
				return nil, err
			}
			runtime.scheduleVariantID = variantID
		}
		if newDay {
			runtime.dayNumber = clock.DayNumber
			runtime.accumulatedDelay = 0
			runtime.blockedBy = ""
			runtime.blockedSince = time.Time{}
			runtime.clearSince = time.Time{}
			runtime.resetAvoidance()
			runtime.lastCorrection = time.Time{}
		} else if freezeSchedule {
			runtime.accumulatedDelay += gameDelta
		}
		runtime.advanceAvoidance(realDelta)
		effectiveSecond := secondsSinceMidnight(clock.GameTime) -
			runtime.accumulatedDelay
		state, err := e.interpreter.EvaluateVariantAt(
			actor,
			runtime.scheduleVariantID,
			effectiveSecond,
			realSecondsPerGameSecond,
		)
		if err != nil {
			return nil, err
		}
		state.AccumulatedDelay = runtime.accumulatedDelay
		state.Revision = runtime.revision
		state.UpdatedAt = clock.ServerTime
		states = append(states, state)
	}

	baseStates := states
	hasAvoiding := false
	for _, state := range states {
		if e.runtimes[state.ID].avoidanceActive() {
			hasAvoiding = true
			baseStates = append([]State(nil), states...)
			break
		}
	}
	for index := range states {
		runtime := e.runtimes[states[index].ID]
		if !runtime.avoidanceActive() {
			continue
		}
		runtime.applyAvoidance(&states[index])
	}
	baseDetected := detectBlockers(baseStates, players)
	detected := baseDetected
	if hasAvoiding {
		detected = detectBlockers(states, players)
	}
	changes := make([]Change, 0)
	for index := range states {
		state := states[index]
		baseState := baseStates[index]
		runtime := e.runtimes[state.ID]
		blocker := detected[state.ID]
		if runtime.avoidanceActive() {
			switch runtime.avoidancePhase {
			case AvoidancePassing:
				if baseDetected[state.ID] == "" {
					if runtime.clearSince.IsZero() {
						runtime.clearSince = clock.ServerTime
					}
					if clock.ServerTime.Sub(runtime.clearSince) >= clearHysteresis {
						runtime.startAvoidanceReturn()
					}
				} else {
					runtime.clearSince = time.Time{}
				}
			case AvoidanceReturning:
				if baseDetected[state.ID] != "" {
					runtime.resumeAvoidanceSidestep()
				}
			}
			runtime.blockedBy = ""
			runtime.blockedSince = time.Time{}
			state = baseState
			runtime.applyAvoidance(&state)
		} else if blocker != "" {
			if runtime.blockedSince.IsZero() {
				runtime.blockedSince = clock.ServerTime
			}
			patience := avoidancePatience
			if strings.HasPrefix(blocker, "npc:") {
				patience = npcAvoidancePatience
			}
			if clock.ServerTime.Sub(runtime.blockedSince) >= patience {
				runtime.startAvoidance(
					chooseAvoidanceOffset(
						baseState,
						baseStates,
						players,
					),
				)
				runtime.blockedBy = ""
				runtime.blockedSince = time.Time{}
				runtime.clearSince = time.Time{}
				state = baseState
				runtime.applyAvoidance(&state)
			} else {
				runtime.blockedBy = blocker
				runtime.clearSince = time.Time{}
			}
		} else if runtime.blockedBy != "" {
			if runtime.clearSince.IsZero() {
				runtime.clearSince = clock.ServerTime
			}
			npcBlocked := strings.HasPrefix(runtime.blockedBy, "npc:")
			if npcBlocked &&
				clock.ServerTime.Sub(runtime.blockedSince) >= npcAvoidancePatience {
				runtime.startAvoidance(
					chooseAvoidanceOffset(baseState, baseStates, players),
				)
				runtime.blockedBy = ""
				runtime.blockedSince = time.Time{}
				state = baseState
				runtime.applyAvoidance(&state)
			} else {
				releaseDelay := clearHysteresis
				if npcBlocked {
					releaseDelay = blockedEncounterReset
				}
				if clock.ServerTime.Sub(runtime.clearSince) >= releaseDelay {
					runtime.blockedBy = ""
				}
			}
		} else {
			// A faster follower can repeatedly cross the obstruction boundary:
			// stop, gain a few centimeters of clearance, resume, then stop again.
			// Keep that as one encounter so threshold chatter cannot continually
			// reset the patience timer and prevent the existing avoidance pass.
			if !runtime.blockedSince.IsZero() &&
				!runtime.clearSince.IsZero() &&
				clock.ServerTime.Sub(runtime.clearSince) >= blockedEncounterReset {
				runtime.blockedSince = time.Time{}
				runtime.clearSince = time.Time{}
			}
		}
		state.AccumulatedDelay = runtime.accumulatedDelay
		state.BlockedBy = runtime.blockedBy
		if runtime.blockedBy != "" && state.Mode == ModeWalking {
			state.Mode = ModeBlocked
		}
		annotateDiscontinuity(runtime.state, &state)
		event := meaningfulChange(runtime.state, state)
		correction := runtime.lastCorrection.IsZero() ||
			clock.ServerTime.Sub(runtime.lastCorrection) >= correctionInterval
		if event || correction {
			previous := runtime.state
			runtime.revision++
			state.Revision = runtime.revision
			runtime.lastCorrection = clock.ServerTime
			changes = append(changes, Change{Previous: previous, Current: state})
		} else {
			state.Revision = runtime.revision
		}
		runtime.state = state
	}
	e.lastTick = clock
	e.started = true
	return changes, nil
}

func annotateDiscontinuity(previous State, current *State) {
	if current == nil {
		return
	}
	previous.Position.X -= previous.AvoidanceOffset.X
	previous.Position.Z -= previous.AvoidanceOffset.Z
	authoredCurrent := *current
	authoredCurrent.Position.X -= authoredCurrent.AvoidanceOffset.X
	authoredCurrent.Position.Z -= authoredCurrent.AvoidanceOffset.Z
	current.TransitionKind, current.TransitionDistance =
		ClassifyDiscontinuity(previous, authoredCurrent)
}

func (e *Engine) Snapshot(worldID string) []State {
	e.mu.RLock()
	defer e.mu.RUnlock()
	states := make([]State, 0)
	for _, actor := range e.actors {
		state := e.runtimes[actor.InstanceID].state
		if state.WorldID == worldID && state.Visible() {
			states = append(states, state)
		}
	}
	return states
}

// ActorPresence returns an explicit fact for every scheduled actor code. A
// false value means that the known actor is not currently resident and visible
// in the requested world; an absent key means the actor is not in this catalog.
func (e *Engine) ActorPresence(worldID string) map[string]bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	presence := make(map[string]bool)
	for _, actor := range e.actors {
		presence[actor.ActorCode] = presence[actor.ActorCode] ||
			(e.runtimes[actor.InstanceID].state.WorldID == worldID &&
				e.runtimes[actor.InstanceID].state.Visible())
	}
	return presence
}

func (e *Engine) Checkpoints() []Checkpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	checkpoints := make([]Checkpoint, 0, len(e.runtimes))
	for id, runtime := range e.runtimes {
		checkpoints = append(checkpoints, Checkpoint{
			NPCID:            id,
			DayNumber:        runtime.dayNumber,
			AccumulatedDelay: runtime.accumulatedDelay,
			BlockedBy:        runtime.blockedBy,
			Revision:         runtime.revision,
			UpdatedAt:        runtime.state.UpdatedAt,
		})
	}
	sort.Slice(checkpoints, func(left, right int) bool {
		return checkpoints[left].NPCID < checkpoints[right].NPCID
	})
	return checkpoints
}

func meaningfulChange(previous, current State) bool {
	if previous.ID == "" {
		return true
	}
	return previous.WorldID != current.WorldID ||
		previous.Mode != current.Mode ||
		previous.Operation != current.Operation ||
		previous.OperationFileOffset != current.OperationFileOffset ||
		previous.RouteID != current.RouteID ||
		previous.RouteSegment != current.RouteSegment ||
		previous.BlockedBy != current.BlockedBy ||
		previous.AvoidancePhase != current.AvoidancePhase ||
		previous.MotionStateID != current.MotionStateID ||
		previous.ScheduleVariantID != current.ScheduleVariantID ||
		previous.ModelOverrideCode != current.ModelOverrideCode
}

func secondsSinceMidnight(value time.Time) float64 {
	hour, minute, second := value.UTC().Clock()
	return float64(hour*3600+minute*60+second) +
		float64(value.UTC().Nanosecond())/float64(time.Second)
}
