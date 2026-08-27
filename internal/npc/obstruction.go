package npc

import (
	"math"
	"sort"
)

const (
	defaultPlayerRadius = 0.45
	defaultNPCRadius    = 0.42
	verticalTolerance   = 1.5
	stopLookahead       = 1.1
)

func detectBlockers(states []State, players []Player) map[string]string {
	blockers := make(map[string]string)
	orderedPlayers := append([]Player(nil), players...)
	sort.Slice(orderedPlayers, func(left, right int) bool {
		return orderedPlayers[left].ID < orderedPlayers[right].ID
	})
	for _, state := range states {
		if state.Mode != ModeWalking || state.WorldID == "" {
			continue
		}
		for _, player := range orderedPlayers {
			if player.WorldID != state.WorldID ||
				math.Abs(player.Position.Y-state.Position.Y) > verticalTolerance {
				continue
			}
			radius := player.Radius
			if radius <= 0 {
				radius = defaultPlayerRadius
			}
			if obstructionAhead(
				state,
				player.Position,
				defaultNPCRadius+radius+stopLookahead,
			) {
				blockers[state.ID] = "player:" + player.ID
				break
			}
		}
	}

	for leftIndex := 0; leftIndex < len(states); leftIndex++ {
		left := states[leftIndex]
		if left.Mode != ModeWalking || left.WorldID == "" {
			continue
		}
		for rightIndex := leftIndex + 1; rightIndex < len(states); rightIndex++ {
			right := states[rightIndex]
			if right.Mode != ModeWalking ||
				right.WorldID != left.WorldID ||
				math.Abs(right.Position.Y-left.Position.Y) > verticalTolerance {
				continue
			}
			maxDistance := defaultNPCRadius*2 + stopLookahead
			if left.Position.horizontalDistance(right.Position) >
				math.Hypot(maxDistance, defaultNPCRadius) {
				continue
			}
			leftSeesRight := obstructionAhead(left, right.Position, maxDistance)
			rightSeesLeft := obstructionAhead(right, left.Position, maxDistance)
			if !leftSeesRight && !rightSeesLeft {
				continue
			}
			switch {
			case leftSeesRight && !rightSeesLeft:
				if blockers[left.ID] == "" {
					blockers[left.ID] = "npc:" + right.ID
				}
			case rightSeesLeft && !leftSeesRight:
				if blockers[right.ID] == "" {
					blockers[right.ID] = "npc:" + left.ID
				}
			case left.ID < right.ID:
				if blockers[right.ID] == "" {
					blockers[right.ID] = "npc:" + left.ID
				}
			default:
				if blockers[left.ID] == "" {
					blockers[left.ID] = "npc:" + right.ID
				}
			}
		}
	}
	return blockers
}

func obstructionAhead(state State, obstacle Vector3, maxDistance float64) bool {
	directionLength := math.Hypot(state.Direction.X, state.Direction.Z)
	if directionLength == 0 {
		return false
	}
	directionX := state.Direction.X / directionLength
	directionZ := state.Direction.Z / directionLength
	deltaX := obstacle.X - state.Position.X
	deltaZ := obstacle.Z - state.Position.Z
	forward := deltaX*directionX + deltaZ*directionZ
	if forward < 0 || forward > maxDistance {
		return false
	}
	lateral := math.Abs(deltaX*directionZ - deltaZ*directionX)
	return lateral <= defaultNPCRadius
}
