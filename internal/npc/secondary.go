package npc

import "math"

// evaluateSecondaryRoute evaluates native operation 0x1c controllers from
// schedule time. That makes the result identical for every client and
// reconstructible after restart; accumulated NPC delay freezes this clock.
func (i *Interpreter) evaluateSecondaryRoute(
	actor *compiledActor,
	variant *compiledVariant,
	journeyIndex int,
	normalizedSecond float64,
	realSecondsPerGameSecond float64,
) (State, bool) {
	if journeyIndex < 0 || realSecondsPerGameSecond <= 0 {
		return State{}, false
	}
	journey := variant.Journeys[journeyIndex]
	routes := make([]compiledOperation, 0)
	area := actor.Source.DefaultArea
	for _, operation := range journey.Operations {
		if operation.Source.Code == 8 {
			area = operation.Payload.Area
		}
		if operation.Source.Code == 0x1c &&
			len(operation.SecondaryPoints) > 0 &&
			operation.Payload.ResolvedPathStepPerUpdate > 0 {
			routes = append(routes, operation)
		}
	}
	if len(routes) == 0 {
		return State{}, false
	}

	type phase struct {
		routeIndex int
		duration   float64
		pause      bool
		handoff    bool
	}
	phases := make([]phase, 0, len(routes)*2)
	cycleDuration := 0.0
	for index, route := range routes {
		speed := route.Payload.ResolvedPathStepPerUpdate *
			nativeMovementUpdatesPerRealSecond
		duration := route.SecondaryLength / speed
		phases = append(phases, phase{routeIndex: index, duration: duration})
		cycleDuration += duration
		pauseUpdates := 2
		if index == len(routes)-1 {
			pauseUpdates = 3
		}
		pauseDuration := float64(pauseUpdates) /
			nativeMovementUpdatesPerRealSecond
		phases = append(phases, phase{
			routeIndex: index,
			duration:   pauseDuration,
			pause:      true,
		})
		cycleDuration += pauseDuration
		if route.SecondaryHandoffLength > 0 &&
			route.Payload.SecondaryHandoff.TargetStepPerUpdate > 0 {
			handoffSpeed := route.Payload.SecondaryHandoff.TargetStepPerUpdate *
				nativeMovementUpdatesPerRealSecond
			handoffDuration := route.SecondaryHandoffLength / handoffSpeed
			phases = append(phases, phase{
				routeIndex: index,
				duration:   handoffDuration,
				handoff:    true,
			})
			cycleDuration += handoffDuration
		}
	}
	if cycleDuration <= 0 {
		return State{}, false
	}

	elapsedReal := math.Max(
		0,
		normalizedSecond-float64(journey.StartSecond),
	) * realSecondsPerGameSecond
	elapsedCycle := math.Mod(elapsedReal, cycleDuration)
	selected := phases[len(phases)-1]
	phaseElapsed := selected.duration
	for _, candidate := range phases {
		if elapsedCycle <= candidate.duration {
			selected = candidate
			phaseElapsed = elapsedCycle
			break
		}
		elapsedCycle -= candidate.duration
	}
	route := routes[selected.routeIndex]
	points := route.SecondaryPoints
	length := route.SecondaryLength
	routeID := route.Source.RouteID
	fileOffset := route.Source.FileOffset
	objectCode := route.Payload.SecondaryObjectCode
	motionStateID := route.Payload.SecondaryControlWord
	stepPerUpdate := route.Payload.ResolvedPathStepPerUpdate
	if selected.handoff {
		points = route.SecondaryHandoffPoints
		length = route.SecondaryHandoffLength
		routeID = route.Payload.SecondaryHandoff.RouteID
		fileOffset = route.Payload.SecondaryHandoff.TargetFileOffset
		objectCode = route.Payload.SecondaryHandoff.TargetObjectCode
		motionStateID = route.Payload.SecondaryHandoff.TargetControlWord
		stepPerUpdate = route.Payload.SecondaryHandoff.TargetStepPerUpdate
	}
	speedReal := stepPerUpdate * nativeMovementUpdatesPerRealSecond
	distance := math.Min(length, phaseElapsed*speedReal)
	if selected.pause {
		distance = route.SecondaryLength
	}
	sampled, ok := sampleRoute(points, distance)
	if !ok {
		return State{}, false
	}
	mode := ModeWalking
	if selected.pause {
		mode = ModeActing
	}
	worldID := i.areaWorlds[area]
	state := State{
		ID:                   actor.Source.InstanceID,
		ActorCode:            actor.Source.ActorCode,
		Label:                actor.Source.Label,
		WorldID:              worldID,
		Area:                 area,
		Position:             sampled.Position,
		HasPosition:          true,
		Direction:            sampled.Direction,
		Yaw:                  sampled.Yaw + math.Pi,
		Mode:                 mode,
		Operation:            0x1c,
		OperationFileOffset:  fileOffset,
		RouteID:              routeID,
		RouteSegment:         sampled.SegmentIndex,
		RouteSegmentProgress: sampled.Progress,
		RouteDistance:        distance,
		RouteLength:          length,
		SpeedPerGameSecond:   speedReal * realSecondsPerGameSecond,
		MotionStateID:        motionStateID,
		ScheduleVariantID:    variant.ID,
		EffectiveSecond:      normalizedSecond,
		Visual: VisualState{
			LocalObjects:         []LocalObject{},
			SecondaryAttachments: []SecondaryAttachment{},
			SecondaryObjectCode:  objectCode,
		},
	}
	return state, worldID != ""
}
