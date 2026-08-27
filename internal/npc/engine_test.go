package npc

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

type memoryCheckpointStore struct {
	checkpoints []Checkpoint
}

func (store *memoryCheckpointStore) LoadNPCCheckpoints(
	context.Context,
) ([]Checkpoint, error) {
	return append([]Checkpoint(nil), store.checkpoints...), nil
}

func (store *memoryCheckpointStore) SaveNPCCheckpoints(
	_ context.Context,
	checkpoints []Checkpoint,
) error {
	store.checkpoints = append([]Checkpoint(nil), checkpoints...)
	return nil
}

func testOperation(t *testing.T, code int, offset string, fields map[string]any) npcdata.Operation {
	t.Helper()
	fields["operation"] = code
	fields["fileOffset"] = offset
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	operation := npcdata.Operation{}
	if err := json.Unmarshal(raw, &operation); err != nil {
		t.Fatal(err)
	}
	return operation
}

func testWalkingManifest(t *testing.T) *npcdata.Manifest {
	t.Helper()
	return &npcdata.Manifest{
		Schema:     "new-yokosuka-server-npcs-v1",
		AreaWorlds: map[string]string{"D000": "dobuita"},
		Actors: []npcdata.Actor{{
			InstanceID:                          "TEST:one",
			ActorCode:                           "TEST",
			Label:                               "Test Walker",
			NativeDefaultPathSpeedPerGameSecond: 1.0 / 30.0,
			DefaultArea:                         "D000",
			Journeys: []npcdata.Journey{{
				StartSecond: 8*3600 + 30*60,
				Operations: []npcdata.Operation{
					testOperation(t, 8, "area", map[string]any{
						"area": "D000",
					}),
					testOperation(t, 1, "route", map[string]any{
						"area":         "D000",
						"movementMode": "0x66",
						"routeId":      "TEST:route",
						"points": [][]float64{
							{0, 0, 0},
							{0, 0, 10_000},
						},
					}),
				},
			}},
		}},
	}
}

func TestEngineReselectsScheduleOnNextCalendarDay(t *testing.T) {
	slots := make([]npcdata.SelectorSlot, 16)
	for index := range slots {
		slots[index] = npcdata.SelectorSlot{
			SelectorIndex:      index,
			RawSchedulePointer: "base",
		}
	}
	manifest := &npcdata.Manifest{
		AreaWorlds: map[string]string{"D000": "dobuita"},
		Actors: []npcdata.Actor{{
			InstanceID:               "TEST:rollover",
			ActorCode:                "TEST",
			Label:                    "Rollover",
			DefaultArea:              "D000",
			DefaultScheduleVariantID: "ordinary",
			ScheduleSelector: npcdata.ScheduleSelector{
				PointerSlots: slots,
				Conditions: []npcdata.ScheduleCondition{{
					StartMonth:           12,
					StartDay:             26,
					RequiredBaseSelector: -1,
					TargetSelectorIndex:  4,
				}},
			},
			ScheduleVariants: []npcdata.ScheduleVariant{
				{
					ScheduleVariantID: "ordinary",
					SelectorIndices:   []int{1},
					Journeys: []npcdata.Journey{{Operations: []npcdata.Operation{
						testOperation(t, 3, "ordinary", map[string]any{
							"worldPosition": []float64{1, 0, 1},
						}),
					}}},
				},
				{
					ScheduleVariantID: "winter",
					SelectorIndices:   []int{4},
					Journeys: []npcdata.Journey{{Operations: []npcdata.Operation{
						testOperation(t, 3, "winter", map[string]any{
							"worldPosition": []float64{2, 0, 2},
						}),
					}}},
				},
			},
		}},
	}
	engine, err := NewEngine(manifest)
	if err != nil {
		t.Fatal(err)
	}
	serverTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	changes, err := engine.Tick(TickTime{
		ServerTime: serverTime,
		GameTime:   time.Date(1986, 12, 25, 8, 30, 0, 0, time.UTC),
		DayNumber:  0,
	}, nil)
	if err != nil || len(changes) != 1 {
		t.Fatalf("first tick changes=%d err=%v", len(changes), err)
	}
	if changes[0].Current.ScheduleVariantID != "ordinary" {
		t.Fatalf("first variant = %s", changes[0].Current.ScheduleVariantID)
	}
	changes, err = engine.Tick(TickTime{
		ServerTime: serverTime.Add(time.Minute),
		GameTime:   time.Date(1986, 12, 26, 8, 30, 0, 0, time.UTC),
		DayNumber:  1,
	}, nil)
	if err != nil || len(changes) != 1 {
		t.Fatalf("rollover changes=%d err=%v", len(changes), err)
	}
	if changes[0].Current.ScheduleVariantID != "winter" ||
		changes[0].Current.Position.X != 2 {
		t.Fatalf("rollover state = %#v", changes[0].Current)
	}
}

func testIntersectionManifest(t *testing.T) *npcdata.Manifest {
	t.Helper()
	routeActor := func(
		id string,
		points [][]float64,
	) npcdata.Actor {
		return npcdata.Actor{
			InstanceID:                          id,
			ActorCode:                           id,
			Label:                               id,
			NativeDefaultPathSpeedPerGameSecond: 1.0 / 30.0,
			DefaultArea:                         "D000",
			Journeys: []npcdata.Journey{{
				StartSecond: 8*3600 + 30*60,
				Operations: []npcdata.Operation{
					testOperation(t, 8, id+":area", map[string]any{
						"area": "D000",
					}),
					testOperation(t, 1, id+":route", map[string]any{
						"area":         "D000",
						"movementMode": "0x66",
						"routeId":      id + ":route",
						"points":       points,
					}),
				},
			}},
		}
	}
	return &npcdata.Manifest{
		Schema:     "new-yokosuka-server-npcs-v1",
		AreaWorlds: map[string]string{"D000": "dobuita"},
		Actors: []npcdata.Actor{
			routeActor("NPC:A", [][]float64{
				{-1_000, 0, 0},
				{10_000, 0, 0},
			}),
			routeActor("NPC:B", [][]float64{
				{0, 0, -1_000},
				{0, 0, 10_000},
			}),
			routeActor("NPC:C", [][]float64{
				{1_000, 0, 0},
				{-10_000, 0, 0},
			}),
		},
	}
}

func testFollowingManifest(t *testing.T) *npcdata.Manifest {
	t.Helper()
	actor := func(id string, startZ, speed float64) npcdata.Actor {
		return npcdata.Actor{
			InstanceID:                          id,
			ActorCode:                           id,
			Label:                               id,
			NativeDefaultPathSpeedPerGameSecond: speed / 30,
			DefaultArea:                         "D000",
			Journeys: []npcdata.Journey{{
				StartSecond: 8*3600 + 30*60,
				Operations: []npcdata.Operation{
					testOperation(t, 8, id+":area", map[string]any{
						"area": "D000",
					}),
					testOperation(t, 1, id+":route", map[string]any{
						"area":         "D000",
						"movementMode": "0x66",
						"routeId":      id + ":route",
						"points": [][]float64{
							{0, 0, startZ},
							{0, 0, startZ + 1_000},
						},
					}),
				},
			}},
		}
	}
	return &npcdata.Manifest{
		Schema:     "new-yokosuka-server-npcs-v1",
		AreaWorlds: map[string]string{"D000": "dobuita"},
		Actors: []npcdata.Actor{
			actor("FOLLOWER", 0, 1),
			actor("LEADER", 1.9, 0.2),
		},
	}
}

func tickTime(server time.Time, game time.Time) TickTime {
	return TickTime{
		ServerTime: server,
		GameTime:   game,
		DayNumber:  4,
	}
}

func TestTwentyMinuteBlockFreezesScheduleWithoutCatchUp(t *testing.T) {
	engine, err := NewEngine(testWalkingManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	serverStart := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	gameStart := time.Date(1986, 6, 13, 8, 30, 0, 0, time.UTC)
	player := Player{
		ID:       "blocking-player",
		WorldID:  "dobuita",
		Position: Vector3{Z: 1},
	}

	if _, err := engine.Tick(tickTime(serverStart, gameStart), []Player{player}); err != nil {
		t.Fatal(err)
	}
	blocked := engine.Snapshot("dobuita")[0]
	if blocked.Mode != ModeBlocked {
		t.Fatalf("initial mode = %s, want blocked", blocked.Mode)
	}

	twentyMinutes := 20 * time.Minute
	if _, err := engine.Tick(
		tickTime(serverStart.Add(time.Second), gameStart.Add(twentyMinutes)),
		[]Player{player},
	); err != nil {
		t.Fatal(err)
	}
	blocked = engine.Snapshot("dobuita")[0]
	if blocked.Position.horizontalDistance(Vector3{}) > 0.001 {
		t.Fatalf("blocked NPC advanced to %+v", blocked.Position)
	}
	if math.Abs(blocked.AccumulatedDelay-twentyMinutes.Seconds()) > 0.001 {
		t.Fatalf("delay = %.3f, want %.3f", blocked.AccumulatedDelay, twentyMinutes.Seconds())
	}

	// Clearing must remain stable for the hysteresis interval.
	if _, err := engine.Tick(
		tickTime(serverStart.Add(1100*time.Millisecond), gameStart.Add(twentyMinutes+100*time.Millisecond)),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Tick(
		tickTime(serverStart.Add(1700*time.Millisecond), gameStart.Add(twentyMinutes+700*time.Millisecond)),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	resumed := engine.Snapshot("dobuita")[0]
	if resumed.Mode != ModeWalking {
		t.Fatalf("resumed mode = %s, want walking", resumed.Mode)
	}
	if resumed.Position.horizontalDistance(Vector3{}) > 0.001 {
		t.Fatalf("NPC caught up on resume to %+v", resumed.Position)
	}

	if _, err := engine.Tick(
		tickTime(serverStart.Add(2700*time.Millisecond), gameStart.Add(twentyMinutes+1700*time.Millisecond)),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	advanced := engine.Snapshot("dobuita")[0]
	if math.Abs(advanced.Position.Z-1) > 0.001 {
		t.Fatalf("resumed position z = %.3f, want 1", advanced.Position.Z)
	}
}

func TestPlayerAlwaysReceivesPriority(t *testing.T) {
	engine, err := NewEngine(testWalkingManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	game := time.Date(1986, 6, 9, 8, 30, 0, 0, time.UTC)
	_, err = engine.Tick(tickTime(now, game), []Player{{
		ID:       "player-1",
		WorldID:  "dobuita",
		Position: Vector3{Z: 1.5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	state := engine.Snapshot("dobuita")[0]
	if state.BlockedBy != "player:player-1" {
		t.Fatalf("blockedBy = %q", state.BlockedBy)
	}
}

func TestBlockedNPCWaitsFifteenSecondsThenSidesteps(t *testing.T) {
	engine, err := NewEngine(testWalkingManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	serverStart := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	gameStart := time.Date(1986, 6, 13, 8, 30, 0, 0, time.UTC)
	player := Player{
		ID:       "blocking-player",
		WorldID:  "dobuita",
		Position: Vector3{Z: 1},
	}

	if _, err := engine.Tick(
		tickTime(serverStart, gameStart),
		[]Player{player},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(14900*time.Millisecond),
			gameStart.Add(14900*time.Millisecond),
		),
		[]Player{player},
	); err != nil {
		t.Fatal(err)
	}
	waiting := engine.Snapshot("dobuita")[0]
	if waiting.Mode != ModeBlocked || waiting.Avoiding {
		t.Fatalf("state before patience = mode %s, avoiding %t", waiting.Mode, waiting.Avoiding)
	}

	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(15100*time.Millisecond),
			gameStart.Add(15100*time.Millisecond),
		),
		[]Player{player},
	); err != nil {
		t.Fatal(err)
	}
	avoiding := engine.Snapshot("dobuita")[0]
	if avoiding.Mode != ModeWalking || !avoiding.Avoiding {
		t.Fatalf("state after patience = mode %s, avoiding %t", avoiding.Mode, avoiding.Avoiding)
	}
	if avoiding.AvoidancePhase != AvoidanceSidestep {
		t.Fatalf("avoidance phase = %q, want sidestep", avoiding.AvoidancePhase)
	}
	if avoiding.BlockedBy != "" {
		t.Fatalf("avoidance retained blocker %q", avoiding.BlockedBy)
	}
	if distance := math.Hypot(
		avoiding.AvoidanceOffset.X,
		avoiding.AvoidanceOffset.Z,
	); distance > 0.001 {
		t.Fatalf("initial avoidance offset distance = %.3f, want 0", distance)
	}
	if math.Abs(avoiding.AccumulatedDelay-15.1) > 0.001 {
		t.Fatalf("delay = %.3f, want 15.1", avoiding.AccumulatedDelay)
	}

	changes, err := engine.Tick(
		tickTime(
			serverStart.Add(15600*time.Millisecond),
			gameStart.Add(15600*time.Millisecond),
		),
		[]Player{player},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("interpolated sidestep emitted %d extra updates", len(changes))
	}
	halfway := engine.Snapshot("dobuita")[0]
	if distance := math.Hypot(
		halfway.AvoidanceOffset.X,
		halfway.AvoidanceOffset.Z,
	); math.Abs(distance-0.5) > 0.001 {
		t.Fatalf("halfway avoidance offset = %.3f, want 0.5", distance)
	}
	if halfway.RouteDistance > 0.001 {
		t.Fatalf("sidestepping advanced route to %.3f", halfway.RouteDistance)
	}

	changes, err = engine.Tick(
		tickTime(
			serverStart.Add(16100*time.Millisecond),
			gameStart.Add(16100*time.Millisecond),
		),
		[]Player{player},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("sidestep completion emitted %d updates, want 1", len(changes))
	}
	atDeviation := engine.Snapshot("dobuita")[0]
	if atDeviation.AvoidancePhase != AvoidancePassing {
		t.Fatalf("avoidance phase = %q, want passing", atDeviation.AvoidancePhase)
	}
	if distance := math.Hypot(
		atDeviation.AvoidanceOffset.X,
		atDeviation.AvoidanceOffset.Z,
	); math.Abs(distance-1) > 0.001 {
		t.Fatalf("full avoidance offset = %.3f, want 1", distance)
	}

	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(17100*time.Millisecond),
			gameStart.Add(17100*time.Millisecond),
		),
		[]Player{player},
	); err != nil {
		t.Fatal(err)
	}
	advanced := engine.Snapshot("dobuita")[0]
	if advanced.Mode != ModeWalking || advanced.RouteDistance < 0.99 {
		t.Fatalf("avoiding NPC did not advance: %#v", advanced)
	}

	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(17200*time.Millisecond),
			gameStart.Add(17200*time.Millisecond),
		),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	clearing := engine.Snapshot("dobuita")[0]
	if clearing.AvoidancePhase != AvoidancePassing {
		t.Fatalf("avoidance phase = %q, want passing", clearing.AvoidancePhase)
	}
	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(17700*time.Millisecond),
			gameStart.Add(17700*time.Millisecond),
		),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	returning := engine.Snapshot("dobuita")[0]
	if returning.AvoidancePhase != AvoidanceReturning {
		t.Fatalf("avoidance phase = %q, want returning", returning.AvoidancePhase)
	}
	if distance := math.Hypot(
		returning.AvoidanceOffset.X,
		returning.AvoidanceOffset.Z,
	); math.Abs(distance-1) > 0.001 {
		t.Fatalf("initial return offset = %.3f, want 1", distance)
	}
	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(18200*time.Millisecond),
			gameStart.Add(18200*time.Millisecond),
		),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	returning = engine.Snapshot("dobuita")[0]
	if distance := math.Hypot(
		returning.AvoidanceOffset.X,
		returning.AvoidanceOffset.Z,
	); math.Abs(distance-0.5) > 0.001 {
		t.Fatalf("return offset = %.3f, want 0.5", distance)
	}
	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(18700*time.Millisecond),
			gameStart.Add(18700*time.Millisecond),
		),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	onRoute := engine.Snapshot("dobuita")[0]
	if onRoute.Avoiding || onRoute.AvoidancePhase != AvoidanceNone {
		t.Fatalf("NPC did not finish returning: %#v", onRoute)
	}
	if distance := math.Hypot(
		onRoute.AvoidanceOffset.X,
		onRoute.AvoidanceOffset.Z,
	); distance > 0.001 {
		t.Fatalf("finished return offset = %.3f, want 0", distance)
	}
}

func TestFollowingThresholdChatterStillReachesAvoidance(t *testing.T) {
	engine, err := NewEngine(testFollowingManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	serverStart := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	gameStart := time.Date(1986, 6, 13, 8, 30, 0, 0, time.UTC)
	for tick := 0; tick <= 20; tick++ {
		elapsed := time.Duration(tick) * 100 * time.Millisecond
		if _, err := engine.Tick(TickTime{
			ServerTime: serverStart.Add(elapsed),
			GameTime:   gameStart.Add(elapsed),
			DayNumber:  4,
			DayLength:  24 * time.Hour,
		}, nil); err != nil {
			t.Fatal(err)
		}
		if tick > 0 && tick < 10 {
			for _, state := range engine.Snapshot("dobuita") {
				if state.ID == "FOLLOWER" && state.Mode != ModeBlocked {
					t.Fatalf(
						"follower resumed during threshold chatter at tick %d: %#v",
						tick,
						state,
					)
				}
			}
		}
	}
	states := engine.Snapshot("dobuita")
	for _, state := range states {
		if state.ID != "FOLLOWER" {
			continue
		}
		if !state.Avoiding || state.AvoidancePhase != AvoidanceSidestep {
			t.Fatalf("follower never escaped threshold chatter: %#v", state)
		}
		return
	}
	t.Fatal("follower state missing")
}

func TestCrowdedIntersectionEscapesWithoutPersistentDeadlock(t *testing.T) {
	engine, err := NewEngine(testIntersectionManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	serverStart := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	gameStart := time.Date(1986, 6, 13, 8, 30, 0, 0, time.UTC)
	player := []Player{{
		ID:       "intersection-player",
		WorldID:  "dobuita",
		Position: Vector3{},
	}}
	if _, err := engine.Tick(
		tickTime(serverStart, gameStart),
		player,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(16*time.Second),
			gameStart.Add(16*time.Second),
		),
		player,
	); err != nil {
		t.Fatal(err)
	}
	for _, state := range engine.Snapshot("dobuita") {
		if state.Mode == ModeBlocked {
			t.Fatalf("%s remained blocked after patience", state.ID)
		}
	}

	// Once the player leaves, the walkers continue rather than retaining a
	// cyclic NPC blocker relationship.
	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(17*time.Second),
			gameStart.Add(17*time.Second),
		),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Tick(
		tickTime(
			serverStart.Add(20*time.Second),
			gameStart.Add(20*time.Second),
		),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	for _, state := range engine.Snapshot("dobuita") {
		if state.Mode == ModeBlocked || state.BlockedBy != "" {
			t.Fatalf("%s remained deadlocked: %#v", state.ID, state)
		}
		if state.RouteDistance < 2.9 {
			t.Fatalf("%s advanced only %.3f units", state.ID, state.RouteDistance)
		}
	}
}

func TestCheckpointRestorePreservesDelayedPosition(t *testing.T) {
	engine, err := NewEngine(testWalkingManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	serverStart := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	gameStart := time.Date(1986, 6, 13, 8, 30, 0, 0, time.UTC)
	blocker := []Player{{
		ID: "player", WorldID: "dobuita", Position: Vector3{Z: 1},
	}}
	if _, err := engine.Tick(tickTime(serverStart, gameStart), blocker); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Tick(
		tickTime(serverStart.Add(time.Second), gameStart.Add(10*time.Minute)),
		blocker,
	); err != nil {
		t.Fatal(err)
	}
	store := &memoryCheckpointStore{}
	if err := store.SaveNPCCheckpoints(context.Background(), engine.Checkpoints()); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewEngine(testWalkingManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Restore(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Tick(
		tickTime(serverStart.Add(2*time.Second), gameStart.Add(10*time.Minute)),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	state := restarted.Snapshot("dobuita")[0]
	if state.Position.horizontalDistance(Vector3{}) > 0.001 {
		t.Fatalf("restored NPC moved to %+v", state.Position)
	}
	if math.Abs(state.AccumulatedDelay-600) > 0.001 {
		t.Fatalf("restored delay = %.3f, want 600", state.AccumulatedDelay)
	}
	if state.BlockedBy != "" {
		t.Fatalf("restored stale blocker %q", state.BlockedBy)
	}
}

func TestDiscontinuitiesAreExplicitlyClassified(t *testing.T) {
	previous := State{
		ID: "TEST", WorldID: "dobuita", Mode: ModeWalking,
		Position: Vector3{}, HasPosition: true, EffectiveSecond: 1,
		SpeedPerGameSecond: 1, Operation: 1,
	}
	current := State{
		ID: "TEST", WorldID: "dobuita", Mode: ModeWalking,
		Position: Vector3{Z: 100}, HasPosition: true, EffectiveSecond: 2,
		SpeedPerGameSecond: 1, Operation: 1,
	}
	annotateDiscontinuity(previous, &current)
	if current.TransitionKind != "authored-schedule-discontinuity" {
		t.Fatalf("transition kind = %q", current.TransitionKind)
	}
	if current.TransitionDistance != 100 {
		t.Fatalf("transition distance = %.3f", current.TransitionDistance)
	}
}

func TestWalkingUpdatesUsePeriodicCorrectionsInsteadOfPerTickBroadcasts(t *testing.T) {
	engine, err := NewEngine(testWalkingManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	serverStart := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	gameStart := time.Date(1986, 6, 13, 8, 30, 0, 0, time.UTC)
	changes, err := engine.Tick(tickTime(serverStart, gameStart), nil)
	if err != nil || len(changes) != 1 {
		t.Fatalf("initial changes = %d, error = %v", len(changes), err)
	}
	changes, err = engine.Tick(
		tickTime(serverStart.Add(time.Second), gameStart.Add(time.Second)),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("ordinary walking produced %d event updates", len(changes))
	}
	changes, err = engine.Tick(
		tickTime(serverStart.Add(2*time.Second), gameStart.Add(2*time.Second)),
		nil,
	)
	if err != nil || len(changes) != 1 {
		t.Fatalf("correction changes = %d, error = %v", len(changes), err)
	}
}
