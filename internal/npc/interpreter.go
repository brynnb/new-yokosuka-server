package npc

import (
	"fmt"
	"math"

	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

const (
	gameDaySeconds                     = 24 * 60 * 60
	nativeMovementUpdatesPerRealSecond = 30
)

func primaryRouteSpeedPerGameSecond(
	actor npcdata.Actor,
	pathStepPerUpdate float64,
	realSecondsPerGameSecond float64,
) float64 {
	if pathStepPerUpdate <= 0 {
		pathStepPerUpdate = actor.NativeDefaultPathSpeedPerGameSecond
	}
	if pathStepPerUpdate <= 0 || realSecondsPerGameSecond <= 0 {
		return 0
	}
	// Native helper 0x0c114cd4 derives operation-1 speed from the active
	// motion's horizontal root displacement per frame. Fall back to the actor's
	// initialization step only for route families without an active motion.
	return pathStepPerUpdate *
		nativeMovementUpdatesPerRealSecond * realSecondsPerGameSecond
}

func (i *Interpreter) Evaluate(
	actor npcdata.Actor,
	effectiveSecond float64,
) (State, error) {
	return i.EvaluateAt(actor, effectiveSecond, 1)
}

func (i *Interpreter) EvaluateAt(
	actor npcdata.Actor,
	effectiveSecond float64,
	realSecondsPerGameSecond float64,
) (State, error) {
	return i.EvaluateVariantAt(
		actor,
		actor.DefaultScheduleVariantID,
		effectiveSecond,
		realSecondsPerGameSecond,
	)
}

func (i *Interpreter) EvaluateVariantAt(
	actor npcdata.Actor,
	variantID string,
	effectiveSecond float64,
	realSecondsPerGameSecond float64,
) (State, error) {
	i.mu.RLock()
	compiled := i.actors[actor.InstanceID]
	i.mu.RUnlock()
	if compiled == nil {
		var err error
		compiled, err = compileActor(actor)
		if err != nil {
			return State{}, err
		}
		i.mu.Lock()
		i.actors[actor.InstanceID] = compiled
		i.mu.Unlock()
	}
	if variantID == "" {
		variantID = compiled.Source.DefaultScheduleVariantID
	}
	variant := compiled.Variants[variantID]
	if variant == nil {
		return State{}, fmt.Errorf(
			"NPC %s has no schedule variant %s",
			actor.InstanceID,
			variantID,
		)
	}
	return i.evaluate(
		compiled,
		variant,
		effectiveSecond,
		realSecondsPerGameSecond,
	)
}

func (i *Interpreter) evaluate(
	actor *compiledActor,
	variant *compiledVariant,
	effectiveSecond float64,
	realSecondsPerGameSecond float64,
) (State, error) {
	source := actor.Source
	routeSpeedPerGameSecond := primaryRouteSpeedPerGameSecond(
		source,
		0,
		realSecondsPerGameSecond,
	)
	state := State{
		ID:                 source.InstanceID,
		ActorCode:          source.ActorCode,
		Label:              source.Label,
		Mode:               ModeHidden,
		MotionStateID:      source.NativeDefaultMotionStateID,
		EffectiveSecond:    effectiveSecond,
		SpeedPerGameSecond: routeSpeedPerGameSecond,
		ScheduleVariantID:  variant.ID,
	}
	if len(variant.Journeys) == 0 {
		return state, nil
	}
	normalizedSecond := math.Mod(effectiveSecond, gameDaySeconds)
	if normalizedSecond < 0 {
		normalizedSecond += gameDaySeconds
	}
	var journey *compiledJourney
	journeyIndex := -1
	for index := len(variant.Journeys) - 1; index >= 0; index-- {
		if float64(variant.Journeys[index].StartSecond) <= normalizedSecond {
			journey = &variant.Journeys[index]
			journeyIndex = index
			break
		}
	}
	if journey == nil {
		return state, nil
	}
	if secondary, ok := i.evaluateSecondaryRoute(
		actor,
		variant,
		journeyIndex,
		normalizedSecond,
		realSecondsPerGameSecond,
	); ok {
		secondary.EffectiveSecond = effectiveSecond
		return secondary, nil
	}
	state.Area = source.DefaultArea
	if state.Area != "" {
		state.WorldID = i.areaWorlds[state.Area]
	}
	state.Mode = ModeActing
	visual := inheritedVisual(actor, variant, journeyIndex)
	state.ModelOverrideCode = visual.modelCode
	descriptorSecond := float64(journey.StartSecond)
	routeCompletionMotionID := 0
	hasRouteCompletionMotion := false
	lifecycleActive := true

	for _, operation := range journey.Operations {
		payload := operation.Payload
		state.Operation = operation.Source.Code
		state.OperationFileOffset = operation.Source.FileOffset
		if position, yaw, placed := visual.apply(operation, state.Area); placed {
			state.Position = position
			state.HasPosition = true
			state.Yaw = yaw
		}
		state.ModelOverrideCode = visual.modelCode
		switch operation.Source.Code {
		case 8:
			state.Area = payload.Area
			state.WorldID = i.areaWorlds[state.Area]
		case 3:
			if position, ok := vectorFromArray(payload.WorldPosition); ok {
				state.Position = position
				state.HasPosition = true
				state.Yaw = -payload.FacingFixed * math.Pi * 2 / 0x10000
				state.Mode = ModeActing
			}
		case 7:
			descriptorSecond += payload.DurationSeconds
			if normalizedSecond < descriptorSecond {
				state.Mode = ModeWaiting
				return finishVisual(state, visual, lifecycleActive), nil
			}
		case 9:
			lifecycleActive = payload.ActorStateValue != 2
			if payload.ActorStateValue != 1 {
				state.MotionStateID = source.NativeDefaultMotionStateID
			}
		case 2, 0x1a:
			state.MotionStateID = payload.MotionStateID
		case 0x19:
			if payload.MotionStateControlWord == 0 {
				state.MotionStateID = 0
			} else {
				state.MotionStateID = payload.MotionStateID
			}
		case 0x17:
			candidates := validInteractionMotions(payload.ControlValues)
			if len(candidates) == 1 {
				state.MotionStateID = candidates[0]
			}
		case 0x2f:
			if payload.ModelOverridePersistent {
				state.ModelOverrideCode = payload.ModelOverrideCode
			} else {
				state.ModelOverrideCode = ""
			}
		case 0x35:
			if payload.RouteCompletionMotionOverrideID != nil {
				routeCompletionMotionID = *payload.RouteCompletionMotionOverrideID
				hasRouteCompletionMotion = true
			} else if payload.RouteCompletionDefaultIdleMotionID != nil {
				routeCompletionMotionID = *payload.RouteCompletionDefaultIdleMotionID
				hasRouteCompletionMotion = true
			}
		case 0x18:
			if payload.GateReleaseSecond <= 0 {
				return State{}, fmt.Errorf(
					"NPC %s operation %s has no gate release",
					source.InstanceID,
					operation.Source.FileOffset,
				)
			}
			if normalizedSecond < payload.GateReleaseSecond {
				state.Mode = ModeWaiting
				return finishVisual(state, visual, lifecycleActive), nil
			}
			descriptorSecond = payload.GateReleaseSecond
		case 0x22:
			releaseSecond := payload.TimeControlValue
			if releaseSecond < 0 {
				releaseSecond = descriptorSecond - releaseSecond
			} else {
				releaseSecond = math.Max(descriptorSecond, releaseSecond)
			}
			if payload.DeterministicMotionStateID != nil {
				state.MotionStateID = *payload.DeterministicMotionStateID
			}
			if normalizedSecond < releaseSecond {
				state.Mode = ModeWaiting
				return finishVisual(state, visual, lifecycleActive), nil
			}
			descriptorSecond = releaseSecond
		case 0x16:
			notBefore := payload.ActivationSecond
			if notBefore < 0 {
				notBefore = float64(journey.StartSecond) - notBefore
			}
			notBefore = math.Max(
				float64(journey.StartSecond)+payload.MinimumDelaySeconds,
				notBefore,
			)
			if normalizedSecond < notBefore {
				if operation.HasWaiting {
					state.Position = operation.WaitingPosition
					state.HasPosition = true
				}
				state.Mode = ModeWaiting
				return finishVisual(state, visual, lifecycleActive), nil
			}
			if len(operation.ControllerPoints) > 0 {
				points := operation.ControllerPoints
				length := operation.ControllerLength
				distance := (normalizedSecond - notBefore) *
					routeSpeedPerGameSecond
				if distance <= length {
					sampled, _ := sampleRoute(points, distance)
					state.Position = sampled.Position
					state.HasPosition = true
					state.Direction = sampled.Direction
					state.Yaw = sampled.Yaw + math.Pi
					state.Mode = ModeWalking
					state.RouteID = payload.LinkedPlacement.ControllerRouteID
					state.RouteSegment = sampled.SegmentIndex
					state.RouteSegmentProgress = sampled.Progress
					state.RouteDistance = distance
					state.RouteLength = length
					return finishVisual(state, visual, lifecycleActive), nil
				}
				descriptorSecond = notBefore + length/
					routeSpeedPerGameSecond
				state.Position = points[len(points)-1]
				state.HasPosition = true
			} else if position, ok := vectorFromArray(
				payload.LinkedPlacement.Position,
			); ok {
				state.Position = position
				state.HasPosition = true
			} else {
				state.HasPosition = false
			}
			if len(operation.HandoffPoints) > 0 {
				points := operation.HandoffPoints
				length := operation.HandoffLength
				distance := (normalizedSecond - descriptorSecond) *
					routeSpeedPerGameSecond
				if distance <= length {
					sampled, _ := sampleRoute(points, distance)
					state.Position = sampled.Position
					state.HasPosition = true
					state.Direction = sampled.Direction
					state.Yaw = sampled.Yaw + math.Pi
					state.Mode = ModeWalking
					state.RouteID = payload.LinkedPlacement.HandoffRouteID
					state.RouteSegment = sampled.SegmentIndex
					state.RouteSegmentProgress = sampled.Progress
					state.RouteDistance = distance
					state.RouteLength = length
					return finishVisual(state, visual, lifecycleActive), nil
				}
				descriptorSecond += length /
					routeSpeedPerGameSecond
				state.Position = points[len(points)-1]
				state.HasPosition = true
			}
		case 1:
			points := operation.Points
			operationRouteSpeed := primaryRouteSpeedPerGameSecond(
				source,
				payload.NativePathStepPerUpdate,
				realSecondsPerGameSecond,
			)
			if len(points) == 0 || operationRouteSpeed <= 0 {
				state.Mode = ModeHidden
				state.WorldID = ""
				return finishVisual(state, visual, lifecycleActive), nil
			}
			state.Area = payload.Area
			if state.Area != "" {
				state.WorldID = i.areaWorlds[state.Area]
			}
			elapsed := math.Max(0, normalizedSecond-descriptorSecond)
			distance := elapsed * operationRouteSpeed
			length := operation.RouteLength
			if distance <= length {
				sampled, _ := sampleRoute(points, distance)
				state.Position = sampled.Position
				state.HasPosition = true
				state.Direction = sampled.Direction
				state.Yaw = sampled.Yaw + math.Pi
				state.Mode = ModeWalking
				state.RouteID = operation.Source.RouteID
				state.RouteSegment = sampled.SegmentIndex
				state.RouteSegmentProgress = sampled.Progress
				state.RouteDistance = distance
				state.RouteLength = length
				state.SpeedPerGameSecond = operationRouteSpeed
				state.MovementMode = payload.MovementMode
				if distance >= length && hasRouteCompletionMotion {
					state.MotionStateID = routeCompletionMotionID
				}
				return finishVisual(state, visual, lifecycleActive), nil
			}
			descriptorSecond += length / operationRouteSpeed
			state.Position = points[len(points)-1]
			state.HasPosition = true
			if len(points) > 1 {
				state.Direction = points[len(points)-2].
					horizontalDirectionTo(points[len(points)-1])
				state.Yaw = math.Atan2(
					state.Direction.X,
					state.Direction.Z,
				) + math.Pi
			}
			if hasRouteCompletionMotion {
				state.MotionStateID = routeCompletionMotionID
			}
		case 0:
			state.Mode = ModeHidden
			state.WorldID = ""
			state.HasPosition = false
			state.RouteID = ""
			return finishVisual(state, visual, lifecycleActive), nil
		case 4, 0x12:
			state.Mode = ModeActing
			return finishVisual(state, visual, lifecycleActive), nil
		}
	}
	return finishVisual(state, visual, lifecycleActive), nil
}

func validInteractionMotions(values []int) []int {
	result := make([]int, 0, 2)
	for _, raw := range values {
		id := raw & 0xffff
		if (id > 0 && id < 0x0618) || (id > 0x8000 && id < 0x8358) {
			result = append(result, id)
		}
		if len(result) == 2 {
			break
		}
	}
	return result
}

func finishVisual(
	state State,
	visual *visualAccumulator,
	lifecycleActive bool,
) State {
	state.Visual = visual.finish()
	if !lifecycleActive {
		state.Mode = ModeHidden
		state.WorldID = ""
		state.HasPosition = false
		state.RouteID = ""
	}
	return state
}
