package npc

import (
	"math"
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

func loadTestManifest(t *testing.T) *npcdata.Manifest {
	t.Helper()
	manifest, err := npcdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func actorByID(t *testing.T, manifest *npcdata.Manifest, id string) npcdata.Actor {
	t.Helper()
	for _, actor := range manifest.Actors {
		if actor.InstanceID == id {
			return actor
		}
	}
	t.Fatalf("actor %s not found", id)
	return npcdata.Actor{}
}

func TestPrimaryRouteUsesRealTimeAndLetsAbsoluteWaitAbsorbDelay(t *testing.T) {
	actor := npcdata.Actor{
		InstanceID:                          "WALK:native-clock",
		ActorCode:                           "WALK",
		NativeDefaultPathSpeedPerGameSecond: 2.0 / 30.0,
		DefaultArea:                         "D000",
		Journeys: []npcdata.Journey{{
			StartSecond: 100,
			Operations: []npcdata.Operation{
				testOperation(t, 8, "area", map[string]any{
					"area": "D000",
				}),
				testOperation(t, 1, "route", map[string]any{
					"area":                    "D000",
					"movementMode":            "0x66",
					"nativePathStepPerUpdate": 1.0 / 30.0,
					"routeId":                 "WALK:route",
					"points": [][]float64{
						{0, 0, 0},
						{0, 0, 10},
					},
				}),
				testOperation(t, 0x18, "absolute-wait", map[string]any{
					"gateReleaseSecond": 150,
				}),
				testOperation(t, 4, "done", map[string]any{}),
			},
		}},
	}
	interpreter := NewInterpreter(map[string]string{"D000": "dobuita"})
	// Fifteen game seconds pass per real second. The ten-metre route still
	// takes ten real seconds (150 game seconds), rather than ten game seconds.
	halfway, err := interpreter.EvaluateAt(actor, 175, 1.0/15.0)
	if err != nil {
		t.Fatal(err)
	}
	if halfway.Mode != ModeWalking || math.Abs(halfway.RouteDistance-5) > 0.001 {
		t.Fatalf("halfway state = %#v, want five metres into route", halfway)
	}
	if math.Abs(halfway.SpeedPerGameSecond-(1.0/15.0)) > 0.001 {
		t.Fatalf("route speed = %.6f, want motion-derived speed", halfway.SpeedPerGameSecond)
	}

	arrived, err := interpreter.EvaluateAt(actor, 250.001, 1.0/15.0)
	if err != nil {
		t.Fatal(err)
	}
	if arrived.Mode != ModeActing {
		t.Fatalf("arrival mode = %s, want acting", arrived.Mode)
	}
	// The authored 00:02:30 gate expired while the actor was traveling, so it
	// adds no extra pause after the late 00:04:10 arrival.
	if arrived.Operation != 4 {
		t.Fatalf("arrival operation = %#x, want completed gate", arrived.Operation)
	}
}

func TestMishimaFamilyEntersHomeAfterEveningRoutes(t *testing.T) {
	manifest := loadTestManifest(t)
	interpreter := NewInterpreter(manifest.AreaWorlds)
	atSevenFortySix := float64(19*3600 + 46*60)
	expectedLifecycle := map[string]int{
		"MAYM:0b9c0e3d8de2": 2,
		"MEGM:681a6e56962e": 0,
		"MISM:9908431895da": 2,
	}
	for id, wantLifecycle := range expectedLifecycle {
		state, err := interpreter.Evaluate(
			actorByID(t, manifest, id),
			atSevenFortySix,
		)
		if err != nil {
			t.Fatal(err)
		}
		if state.Visible() || state.Mode != ModeHidden {
			t.Fatalf(
				"%s at 19:46 = mode %s world %q position %+v, want hidden",
				id,
				state.Mode,
				state.WorldID,
				state.Position,
			)
		}
		if state.Visual.ActorLifecycleControlState == nil ||
			*state.Visual.ActorLifecycleControlState != wantLifecycle {
			t.Fatalf(
				"%s lifecycle = %v, want %d",
				id,
				state.Visual.ActorLifecycleControlState,
				wantLifecycle,
			)
		}
	}
}

func TestLifecycleTwoHidesUntilAFollowingLifecycleActivation(t *testing.T) {
	actor := npcdata.Actor{
		InstanceID:  "LIFE:test",
		ActorCode:   "LIFE",
		DefaultArea: "JD00",
		Journeys: []npcdata.Journey{{
			StartSecond: 0,
			Operations: []npcdata.Operation{
				testOperation(t, 3, "outside", map[string]any{
					"worldPosition": []float64{1, 0, 2},
				}),
				testOperation(t, 9, "inside", map[string]any{
					"actorStateValue": 2,
				}),
				testOperation(t, 7, "inside-wait", map[string]any{
					"durationSeconds": 100,
				}),
				testOperation(t, 9, "outside-again", map[string]any{
					"actorStateValue": 1,
				}),
				testOperation(t, 3, "new-position", map[string]any{
					"worldPosition": []float64{3, 0, 4},
				}),
				testOperation(t, 4, "stay-active", map[string]any{}),
			},
		}},
	}
	interpreter := NewInterpreter(map[string]string{"JD00": "sakuragaoka"})

	hidden, err := interpreter.Evaluate(actor, 50)
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Visible() || hidden.Mode != ModeHidden {
		t.Fatalf("lifecycle 2 state = %+v, want hidden", hidden)
	}

	active, err := interpreter.Evaluate(actor, 101)
	if err != nil {
		t.Fatal(err)
	}
	if !active.Visible() || active.Mode != ModeActing {
		t.Fatalf("reactivated state = %+v, want visible acting", active)
	}
}

func TestOperationZeroEndsWorldResidency(t *testing.T) {
	actor := npcdata.Actor{
		InstanceID:  "END:test",
		ActorCode:   "END",
		DefaultArea: "JD00",
		Journeys: []npcdata.Journey{{
			StartSecond: 0,
			Operations: []npcdata.Operation{
				testOperation(t, 3, "door", map[string]any{
					"worldPosition": []float64{1, 0, 2},
				}),
				testOperation(t, 0, "end", map[string]any{}),
			},
		}},
	}
	state, err := NewInterpreter(
		map[string]string{"JD00": "sakuragaoka"},
	).Evaluate(actor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if state.Visible() || state.Mode != ModeHidden || state.HasPosition {
		t.Fatalf("terminated state = %+v, want non-resident", state)
	}
}

func TestItoLinkedControllerWalksInsteadOfWarping(t *testing.T) {
	manifest := loadTestManifest(t)
	interpreter := NewInterpreter(manifest.AreaWorlds)
	ito := actorByID(t, manifest, "ITOH:adb3e19e82a8")
	realSecondsPerGameSecond := 1.0 / 15.0

	waiting, err := interpreter.EvaluateAt(
		ito,
		16*3600+58*60,
		realSecondsPerGameSecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Mode != ModeWaiting {
		t.Fatalf("16:58 mode = %s, want waiting", waiting.Mode)
	}

	started, err := interpreter.EvaluateAt(
		ito,
		17*3600,
		realSecondsPerGameSecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if started.Mode != ModeWalking {
		t.Fatalf("17:00 mode = %s, want walking", started.Mode)
	}
	if started.RouteID == "" {
		t.Fatal("linked controller route has no stable ID")
	}
	if distance := waiting.Position.horizontalDistance(started.Position); distance > 0.01 {
		t.Fatalf("linked controller warped %.3fm at activation", distance)
	}

	continued, err := interpreter.EvaluateAt(
		ito,
		17*3600+10,
		realSecondsPerGameSecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if continued.RouteID != started.RouteID {
		t.Fatalf("controller route changed from %q to %q", started.RouteID, continued.RouteID)
	}
	if continued.RouteDistance <= started.RouteDistance {
		t.Fatal("controller route did not advance")
	}

	previous := started
	for second := 1.0; second <= 120; second++ {
		current, err := interpreter.EvaluateAt(
			ito,
			17*3600+second,
			realSecondsPerGameSecond,
		)
		if err != nil {
			t.Fatal(err)
		}
		distance := previous.Position.horizontalDistance(current.Position)
		maximumDistance := primaryRouteSpeedPerGameSecond(
			ito,
			0,
			realSecondsPerGameSecond,
		)
		if distance > maximumDistance+0.01 {
			t.Fatalf(
				"Ito moved %.3fm between 17:%02.0f and 17:%02.0f",
				distance,
				second-1,
				second,
			)
		}
		previous = current
	}
}

func TestEveryActorProducesFiniteStatesAcrossAFullDay(t *testing.T) {
	manifest := loadTestManifest(t)
	interpreter := NewInterpreter(manifest.AreaWorlds)
	for _, actor := range manifest.Actors {
		for second := 0.0; second < gameDaySeconds; second += 60 {
			state, err := interpreter.Evaluate(actor, second)
			if err != nil {
				t.Fatalf("%s at %.0f: %v", actor.InstanceID, second, err)
			}
			for name, value := range map[string]float64{
				"x":   state.Position.X,
				"y":   state.Position.Y,
				"z":   state.Position.Z,
				"yaw": state.Yaw,
			} {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatalf("%s at %.0f has invalid %s", actor.InstanceID, second, name)
				}
			}
		}
	}
}

func TestSecondaryControllerUsesStableServerRouteState(t *testing.T) {
	manifest := loadTestManifest(t)
	interpreter := NewInterpreter(manifest.AreaWorlds)
	actor := actorByID(t, manifest, "FLD5:39f7fc1009d7")
	const start = 46800
	const realSecondsPerGameSecond = 4.0 / 60.0
	initial, err := interpreter.EvaluateAt(
		actor,
		start,
		realSecondsPerGameSecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	next, err := interpreter.EvaluateAt(
		actor,
		start+1,
		realSecondsPerGameSecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Operation != 0x1c || initial.RouteID == "" {
		t.Fatalf("secondary state = op %#x route %q", initial.Operation, initial.RouteID)
	}
	if initial.Visual.SecondaryObjectCode == "" {
		t.Fatal("secondary object code was not sent by the interpreter")
	}
	distance := initial.Position.horizontalDistance(next.Position)
	if distance <= 0 || distance > next.SpeedPerGameSecond+0.01 {
		t.Fatalf(
			"secondary controller moved %.3fm at speed %.3f",
			distance,
			next.SpeedPerGameSecond,
		)
	}
}

func TestSecondaryNearbyHandoffsRemainContinuousAcrossAFullDay(t *testing.T) {
	manifest := loadTestManifest(t)
	interpreter := NewInterpreter(manifest.AreaWorlds)
	for _, actor := range manifest.Actors {
		hasSecondaryRoute := false
		for _, journey := range actor.Journeys {
			for _, operation := range journey.Operations {
				if operation.Code == 0x1c {
					hasSecondaryRoute = true
					break
				}
			}
		}
		if !hasSecondaryRoute {
			continue
		}
		var previous State
		for second := 0.0; second < gameDaySeconds; second++ {
			current, err := interpreter.Evaluate(actor, second)
			if err != nil {
				t.Fatal(err)
			}
			kind, distance := ClassifyDiscontinuity(previous, current)
			if previous.Operation == 0x1c && current.Operation == 0x1c &&
				kind != "" && distance <= 10 {
				t.Fatalf(
					"%s has %.3fm nearby secondary warp at %.0f (%s)",
					actor.InstanceID,
					distance,
					second,
					kind,
				)
			}
			previous = current
		}
	}
}

func TestVisualOperationsAreResolvedServerSide(t *testing.T) {
	actor := npcdata.Actor{
		InstanceID:                 "VISUAL:test",
		ActorCode:                  "VISU",
		DefaultArea:                "D000",
		NativeDefaultMotionStateID: 1,
		Journeys: []npcdata.Journey{{
			StartSecond: 0,
			Operations: []npcdata.Operation{
				testOperation(t, 8, "area", map[string]any{"area": "D000"}),
				testOperation(t, 3, "place", map[string]any{
					"worldPosition": []float64{1, 2, 3},
				}),
				testOperation(t, 0x10, "object", map[string]any{
					"localTransform": map[string]any{
						"objectCode": "ITEM", "locationCode": "BA06",
						"controlWord":           1,
						"runtimePosition":       []float64{0, 0, 0},
						"worldPosition":         []float64{0, 0, 0},
						"transformControlWords": []int{1, 2, 3},
					},
				}),
				testOperation(t, 0x30, "action", map[string]any{
					"actionControllerId":   33219,
					"actionControllerMode": 7,
				}),
				testOperation(t, 0, "end", map[string]any{}),
			},
		}},
	}
	interpreter := NewInterpreter(map[string]string{"D000": "dobuita"})
	state, err := interpreter.Evaluate(actor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Visual.LocalObjects) != 1 ||
		state.Visual.LocalObjects[0].ObjectCode != "ITEM" ||
		state.Visual.LocalObjects[0].ControlWord != 1 {
		t.Fatalf("local objects = %+v", state.Visual.LocalObjects)
	}
	if state.Visual.ActionControllerID == nil ||
		*state.Visual.ActionControllerID != 33219 {
		t.Fatalf("action controller = %+v", state.Visual.ActionControllerID)
	}
}
