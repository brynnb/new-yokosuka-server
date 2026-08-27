package npc

import (
	"math"
	"sort"
)

type visualAccumulator struct {
	state       VisualState
	local       map[string]LocalObject
	attachments map[string]SecondaryAttachment
	modelCode   string
}

func newVisualAccumulator() *visualAccumulator {
	return &visualAccumulator{
		state: VisualState{
			LocalObjects:         []LocalObject{},
			SecondaryAttachments: []SecondaryAttachment{},
		},
		local:       make(map[string]LocalObject),
		attachments: make(map[string]SecondaryAttachment),
	}
}

func (visual *visualAccumulator) apply(
	operation compiledOperation,
	area string,
) (position Vector3, yaw float64, hasPlacement bool) {
	payload := operation.Payload
	switch operation.Source.Code {
	case 9:
		value := payload.ActorStateValue
		visual.state.ActorLifecycleControlState = &value
	case 0x0f:
		value := payload.ActorControlValue
		visual.state.ActorQueryState = &value
	case 0x28:
		value := 0
		if payload.ActorByteValue == 1 {
			value = 1
		}
		visual.state.ActorBooleanControllerMode = &value
	case 0x38:
		value := 0
		if payload.ActorBooleanValue {
			value = 1
		}
		visual.state.ActorBoundsControlMode = &value
	case 0x17:
		visual.state.InteractionTargetCode = payload.TargetCode
	case 0x10:
		transform := payload.LocalTransform
		if transform.ObjectCode != "" {
			visual.local[transform.ObjectCode] = LocalObject{
				ObjectCode:            transform.ObjectCode,
				LocationCode:          transform.LocationCode,
				Area:                  area,
				ControlWord:           transform.ControlWord,
				PlacementMode:         transform.PlacementMode,
				RuntimePosition:       append([]float64(nil), transform.RuntimePosition...),
				WorldPosition:         append([]float64(nil), transform.WorldPosition...),
				TransformControlWords: append([]int(nil), transform.TransformControlWords...),
				ResolvedModel:         transform.ResolvedModel,
				SourceOffset:          operation.Source.FileOffset,
			}
		}
	case 0x11:
		delete(visual.local, payload.ObjectCode)
	case 0x2f:
		if payload.ModelOverridePersistent {
			visual.modelCode = payload.ModelOverrideCode
		} else {
			visual.modelCode = ""
		}
	case 0x30:
		if payload.ActionControllerID == 0 {
			visual.state.ActionControllerID = nil
			visual.state.ActionControllerMode = nil
		} else {
			id, mode := payload.ActionControllerID, payload.ActionControllerMode
			visual.state.ActionControllerID = &id
			visual.state.ActionControllerMode = &mode
		}
	case 0x24:
		if !payload.Enabled {
			delete(visual.attachments, payload.SecondaryObjectCode)
			break
		}
		if value, ok := vectorFromArray(payload.WorldVector); ok {
			rootYaw := -float64(payload.TransformControlWord) *
				math.Pi * 2 / 0x10000
			visual.attachments[payload.SecondaryObjectCode] = SecondaryAttachment{
				ObjectCode:           payload.SecondaryObjectCode,
				Area:                 area,
				Position:             append([]float64(nil), payload.WorldVector...),
				RootYaw:              rootYaw,
				TransformControlWord: payload.TransformControlWord,
				SourceOffset:         operation.Source.FileOffset,
			}
			return value, rootYaw, true
		}
	}
	return Vector3{}, 0, false
}

func (visual *visualAccumulator) finish() VisualState {
	visual.state.LocalObjects = make([]LocalObject, 0, len(visual.local))
	for _, object := range visual.local {
		visual.state.LocalObjects = append(visual.state.LocalObjects, object)
	}
	sort.Slice(visual.state.LocalObjects, func(left, right int) bool {
		return visual.state.LocalObjects[left].ObjectCode <
			visual.state.LocalObjects[right].ObjectCode
	})
	visual.state.SecondaryAttachments = make(
		[]SecondaryAttachment,
		0,
		len(visual.attachments),
	)
	for _, attachment := range visual.attachments {
		visual.state.SecondaryAttachments = append(
			visual.state.SecondaryAttachments,
			attachment,
		)
	}
	sort.Slice(visual.state.SecondaryAttachments, func(left, right int) bool {
		return visual.state.SecondaryAttachments[left].ObjectCode <
			visual.state.SecondaryAttachments[right].ObjectCode
	})
	return visual.state
}

func inheritedVisual(
	actor *compiledActor,
	variant *compiledVariant,
	currentJourneyIndex int,
) *visualAccumulator {
	visual := newVisualAccumulator()
	area := actor.Source.DefaultArea
	for index := 0; index < currentJourneyIndex; index++ {
		journey := variant.Journeys[index]
		nextStart := float64(variant.Journeys[index+1].StartSecond)
		for _, operation := range journey.Operations {
			if activation := operation.Payload.DescriptorActivationSecond; activation != nil && *activation >= nextStart {
				continue
			}
			if operation.Source.Code == 8 {
				area = operation.Payload.Area
				continue
			}
			switch operation.Source.Code {
			case 0x10, 0x11, 0x24:
				// These object registrations persist across timetable entries.
			case 0x2f:
				// A model override is inherited only when extraction resolved
				// its activation before the following timetable entry.
				if operation.Payload.DescriptorActivationSecond == nil {
					continue
				}
			default:
				continue
			}
			visual.apply(operation, area)
		}
	}
	return visual
}
