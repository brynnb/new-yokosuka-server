package npc

import "testing"

func walkingState(id string, position, direction Vector3) State {
	return State{
		ID:          id,
		WorldID:     "dobuita",
		Position:    position,
		HasPosition: true,
		Direction:   direction,
		Mode:        ModeWalking,
	}
}

func TestHeadOnNPCsYieldByStableIDRegardlessOfInputOrder(t *testing.T) {
	first := walkingState(
		"NPC:A",
		Vector3{Z: -0.5},
		Vector3{Z: 1},
	)
	second := walkingState(
		"NPC:B",
		Vector3{Z: 0.5},
		Vector3{Z: -1},
	)
	for _, states := range [][]State{
		{first, second},
		{second, first},
	} {
		blockers := detectBlockers(states, nil)
		if blockers["NPC:A"] != "" {
			t.Fatalf("priority NPC unexpectedly blocked: %#v", blockers)
		}
		if blockers["NPC:B"] != "npc:NPC:A" {
			t.Fatalf("yield decision = %#v", blockers)
		}
	}
}

func TestNPCFollowingAnotherNPCYieldsToTheLeader(t *testing.T) {
	follower := walkingState(
		"NPC:A",
		Vector3{},
		Vector3{Z: 1},
	)
	leader := walkingState(
		"NPC:B",
		Vector3{Z: 1},
		Vector3{Z: 1},
	)
	blockers := detectBlockers([]State{follower, leader}, nil)
	if blockers["NPC:A"] != "npc:NPC:B" {
		t.Fatalf("follower yield decision = %#v", blockers)
	}
	if blockers["NPC:B"] != "" {
		t.Fatalf("leader unexpectedly blocked: %#v", blockers)
	}
}

func TestForwardObstructionUsesNPCWidthRectangle(t *testing.T) {
	state := walkingState(
		"NPC:A",
		Vector3{},
		Vector3{Z: 1},
	)
	const forwardRange = defaultNPCRadius + defaultPlayerRadius + stopLookahead
	if !obstructionAhead(
		state,
		Vector3{X: defaultNPCRadius, Z: forwardRange},
		forwardRange,
	) {
		t.Fatal("obstacle on the forward rectangle edge was not detected")
	}
	if obstructionAhead(
		state,
		Vector3{X: defaultNPCRadius + 0.01, Z: 0.5},
		forwardRange,
	) {
		t.Fatal("obstacle beside the forward rectangle was detected")
	}
	if obstructionAhead(
		state,
		Vector3{Z: -0.1},
		forwardRange,
	) {
		t.Fatal("obstacle behind the NPC was detected")
	}
}

func TestPlayerBesideNPCDoesNotBlockNarrowForwardLane(t *testing.T) {
	state := walkingState(
		"NPC:A",
		Vector3{},
		Vector3{Z: 1},
	)
	blockers := detectBlockers([]State{state}, []Player{{
		ID:       "beside",
		WorldID:  "dobuita",
		Position: Vector3{X: 0.75, Z: 0.5},
	}})
	if blockers[state.ID] != "" {
		t.Fatalf("side player unexpectedly blocked NPC: %#v", blockers)
	}
}
