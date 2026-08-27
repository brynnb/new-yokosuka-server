package npc

import (
	"hash/fnv"
	"math"
	"time"
)

const (
	avoidanceOffsetDistance = 1.0
	avoidanceSidestepSpeed  = 1.0
)

func (runtime *runtimeState) avoidanceActive() bool {
	return runtime.avoidancePhase != AvoidanceNone
}

func (runtime *runtimeState) startAvoidance(target Vector3) {
	runtime.avoidancePhase = AvoidanceSidestep
	runtime.avoidanceOffset = Vector3{}
	runtime.avoidanceTarget = target
	runtime.avoidanceMotion = 0
	runtime.clearSince = time.Time{}
}

func (runtime *runtimeState) startAvoidanceReturn() {
	runtime.avoidancePhase = AvoidanceReturning
	runtime.avoidanceMotion = 0
	runtime.clearSince = time.Time{}
}

func (runtime *runtimeState) resumeAvoidanceSidestep() {
	runtime.avoidancePhase = AvoidanceSidestep
	runtime.avoidanceMotion = 0
	runtime.clearSince = time.Time{}
}

func (runtime *runtimeState) resetAvoidance() {
	runtime.avoidancePhase = AvoidanceNone
	runtime.avoidanceOffset = Vector3{}
	runtime.avoidanceTarget = Vector3{}
	runtime.avoidanceMotion = 0
}

func (runtime *runtimeState) advanceAvoidance(realDelta float64) {
	if realDelta <= 0 {
		return
	}
	switch runtime.avoidancePhase {
	case AvoidanceSidestep:
		runtime.avoidanceMotion += realDelta
		runtime.avoidanceOffset = moveHorizontalToward(
			runtime.avoidanceOffset,
			runtime.avoidanceTarget,
			avoidanceSidestepSpeed*realDelta,
		)
		if runtime.avoidanceOffset == runtime.avoidanceTarget {
			runtime.avoidancePhase = AvoidancePassing
		}
	case AvoidanceReturning:
		runtime.avoidanceMotion += realDelta
		runtime.avoidanceOffset = moveHorizontalToward(
			runtime.avoidanceOffset,
			Vector3{},
			avoidanceSidestepSpeed*realDelta,
		)
		if runtime.avoidanceOffset == (Vector3{}) {
			runtime.resetAvoidance()
		}
	}
}

func (runtime *runtimeState) applyAvoidance(state *State) {
	if state == nil || !runtime.avoidanceActive() {
		return
	}
	state.Position = state.Position.addScaled(runtime.avoidanceOffset, 1)
	state.Avoiding = true
	state.AvoidancePhase = runtime.avoidancePhase
	state.AvoidanceOffset = runtime.avoidanceOffset
	state.AvoidanceTarget = runtime.avoidanceTarget
	state.AvoidanceSpeed = avoidanceSidestepSpeed
	state.AvoidanceMotionTime = runtime.avoidanceMotion
	switch runtime.avoidancePhase {
	case AvoidanceSidestep:
		setAvoidanceFacing(state, runtime.avoidanceTarget)
	case AvoidanceReturning:
		setAvoidanceFacing(state, Vector3{
			X: -runtime.avoidanceTarget.X,
			Z: -runtime.avoidanceTarget.Z,
		})
	}
}

func setAvoidanceFacing(state *State, direction Vector3) {
	length := math.Hypot(direction.X, direction.Z)
	if length == 0 {
		return
	}
	state.Direction = Vector3{
		X: direction.X / length,
		Z: direction.Z / length,
	}
	state.Yaw = math.Atan2(state.Direction.X, state.Direction.Z) + math.Pi
}

func moveHorizontalToward(current, target Vector3, maximum float64) Vector3 {
	deltaX := target.X - current.X
	deltaZ := target.Z - current.Z
	distance := math.Hypot(deltaX, deltaZ)
	if distance == 0 || distance <= maximum {
		return target
	}
	scale := maximum / distance
	return Vector3{
		X: current.X + deltaX*scale,
		Z: current.Z + deltaZ*scale,
	}
}

// chooseAvoidanceOffset selects a deterministic one-unit sidestep. The server
// does not yet have collision geometry, so nearby actors are used only to pick
// the less crowded side of the authored route.
func chooseAvoidanceOffset(
	state State,
	states []State,
	players []Player,
) Vector3 {
	directionLength := math.Hypot(state.Direction.X, state.Direction.Z)
	if directionLength == 0 {
		return Vector3{}
	}
	perpendicular := Vector3{
		X: -state.Direction.Z / directionLength,
		Z: state.Direction.X / directionLength,
	}
	left := Vector3{
		X: perpendicular.X * avoidanceOffsetDistance,
		Z: perpendicular.Z * avoidanceOffsetDistance,
	}
	right := Vector3{X: -left.X, Z: -left.Z}

	leftScore := avoidanceClearance(state, left, states, players)
	rightScore := avoidanceClearance(state, right, states, players)
	switch {
	case leftScore > rightScore:
		return left
	case rightScore > leftScore:
		return right
	case stableAvoidanceSide(state.ID) < 0:
		return right
	default:
		return left
	}
}

func avoidanceClearance(
	state State,
	offset Vector3,
	states []State,
	players []Player,
) float64 {
	candidate := state.Position.addScaled(offset, 1)
	closest := math.Inf(1)
	for _, other := range states {
		if other.ID == state.ID ||
			other.WorldID != state.WorldID ||
			!other.Visible() ||
			math.Abs(other.Position.Y-state.Position.Y) > verticalTolerance {
			continue
		}
		closest = math.Min(closest, candidate.horizontalDistance(other.Position))
	}
	for _, player := range players {
		if player.WorldID != state.WorldID ||
			math.Abs(player.Position.Y-state.Position.Y) > verticalTolerance {
			continue
		}
		closest = math.Min(closest, candidate.horizontalDistance(player.Position))
	}
	return closest
}

func stableAvoidanceSide(id string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	if hash.Sum32()%2 == 0 {
		return -1
	}
	return 1
}
