package npc

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

type Interpreter struct {
	areaWorlds map[string]string
	mu         sync.RWMutex
	actors     map[string]*compiledActor
}

func NewInterpreter(areaWorlds map[string]string) *Interpreter {
	copied := make(map[string]string, len(areaWorlds))
	for area, worldID := range areaWorlds {
		copied[area] = worldID
	}
	return &Interpreter{
		areaWorlds: copied,
		actors:     make(map[string]*compiledActor),
	}
}

type operationPayload struct {
	Area                               string          `json:"area"`
	WorldPosition                      []float64       `json:"worldPosition"`
	FacingFixed                        float64         `json:"facingFixed"`
	DurationSeconds                    float64         `json:"durationSeconds"`
	ActorStateValue                    int             `json:"actorStateValue"`
	ActorControlValue                  int             `json:"actorControlValue"`
	MotionStateID                      int             `json:"motionStateId"`
	MotionStateControlWord             int             `json:"motionStateControlWord"`
	MovementMode                       string          `json:"movementMode"`
	NativePathStepPerUpdate            float64         `json:"nativePathStepPerUpdate"`
	Points                             [][]float64     `json:"points"`
	ActivationSecond                   float64         `json:"activationSecond"`
	MinimumDelaySeconds                float64         `json:"minimumDelaySeconds"`
	TargetCode                         string          `json:"targetCode"`
	TimeControlValue                   float64         `json:"timeControlValue"`
	GateReleaseSecond                  float64         `json:"gateReleaseSecond"`
	DeterministicMotionStateID         *int            `json:"deterministicMotionStateId"`
	ModelOverrideCode                  string          `json:"modelOverrideCode"`
	ModelOverridePersistent            bool            `json:"modelOverridePersistent"`
	RouteCompletionMotionOverrideID    *int            `json:"routeCompletionMotionOverrideId"`
	RouteCompletionDefaultIdleMotionID *int            `json:"routeCompletionDefaultIdleUnanimousMotionId"`
	LinkedPlacement                    linkedPlacement `json:"linkedPlacement"`
	ActorByteValue                     int             `json:"actorByteValue"`
	ActorBooleanValue                  bool            `json:"actorBooleanValue"`
	ControlValues                      []int           `json:"controlValues"`
	ObjectCode                         string          `json:"objectCode"`
	ResidentCharacterCode              string          `json:"residentCharacterCode"`
	ResidentCharacterIndex             int             `json:"residentCharacterIndex"`
	SceneObjectCode                    string          `json:"sceneObjectCode"`
	SceneObjectControlValue            int             `json:"sceneObjectControlValue"`
	SceneObjectTransitionMode          int             `json:"sceneObjectTransitionMode"`
	ActionControllerID                 int             `json:"actionControllerId"`
	ActionControllerMode               int             `json:"actionControllerMode"`
	Enabled                            bool            `json:"enabled"`
	WorldVector                        []float64       `json:"worldVector"`
	TransformControlWord               int             `json:"transformControlWord"`
	SecondaryControlWord               int             `json:"secondaryControlWord"`
	SecondaryObjectCode                string          `json:"secondaryObjectCode"`
	ResolvedPathStepPerUpdate          float64         `json:"resolvedPathStepPerUpdate"`
	DescriptorActivationSecond         *float64        `json:"descriptorActivationSecond"`
	LocalTransform                     localTransform  `json:"localTransform"`
	SecondaryRoute                     struct {
		Points [][]float64 `json:"points"`
	} `json:"secondaryRoute"`
	SecondaryHandoff struct {
		RouteID             string      `json:"routeId"`
		Points              [][]float64 `json:"points"`
		TargetFileOffset    string      `json:"targetFileOffset"`
		TargetObjectCode    string      `json:"targetObjectCode"`
		TargetControlWord   int         `json:"targetControlWord"`
		TargetStepPerUpdate float64     `json:"targetStepPerUpdate"`
	} `json:"secondaryHandoff"`
	WaitingPlacement struct {
		Position []float64 `json:"position"`
	} `json:"waitingPlacement"`
}

type localTransform struct {
	ObjectCode            string    `json:"objectCode"`
	LocationCode          string    `json:"locationCode"`
	ControlWord           int       `json:"controlWord"`
	PlacementMode         int       `json:"placementMode"`
	RuntimePosition       []float64 `json:"runtimePosition"`
	WorldPosition         []float64 `json:"worldPosition"`
	TransformControlWords []int     `json:"transformControlWords"`
	ResolvedModel         string    `json:"resolvedModel"`
}

type linkedPlacement struct {
	Position              []float64   `json:"position"`
	ControllerRouteID     string      `json:"controllerRouteId"`
	ControllerRoutePoints [][]float64 `json:"controllerRoutePoints"`
	HandoffRouteID        string      `json:"handoffRouteId"`
	HandoffRoutePoints    [][]float64 `json:"handoffRoutePoints"`
}

type compiledOperation struct {
	Source                 npcdata.Operation
	Payload                operationPayload
	Points                 []Vector3
	RouteLength            float64
	ControllerPoints       []Vector3
	ControllerLength       float64
	HandoffPoints          []Vector3
	HandoffLength          float64
	SecondaryPoints        []Vector3
	SecondaryLength        float64
	SecondaryHandoffPoints []Vector3
	SecondaryHandoffLength float64
	WaitingPosition        Vector3
	HasWaiting             bool
}

type compiledJourney struct {
	StartSecond int
	Operations  []compiledOperation
}

type compiledActor struct {
	Source   npcdata.Actor
	Variants map[string]*compiledVariant
}

type compiledVariant struct {
	ID       string
	Journeys []compiledJourney
}

func decodeOperation(operation npcdata.Operation) (operationPayload, error) {
	var payload operationPayload
	if err := json.Unmarshal(operation.Payload, &payload); err != nil {
		return payload, fmt.Errorf(
			"decode NPC operation %d at %s: %w",
			operation.Code,
			operation.FileOffset,
			err,
		)
	}
	return payload, nil
}

func compileJourneys(journeys []npcdata.Journey) ([]compiledJourney, error) {
	compiled := make([]compiledJourney, 0, len(journeys))
	for _, journey := range journeys {
		nextJourney := compiledJourney{
			StartSecond: journey.StartSecond,
			Operations:  make([]compiledOperation, 0, len(journey.Operations)),
		}
		for _, operation := range journey.Operations {
			payload, err := decodeOperation(operation)
			if err != nil {
				return nil, err
			}
			next := compiledOperation{Source: operation, Payload: payload}
			next.Points, _ = decodePoints(payload.Points)
			next.RouteLength = routeLength(next.Points)
			next.ControllerPoints, _ = decodePoints(
				payload.LinkedPlacement.ControllerRoutePoints,
			)
			next.WaitingPosition, next.HasWaiting = vectorFromArray(
				payload.WaitingPlacement.Position,
			)
			next.ControllerLength = routeLength(next.ControllerPoints)
			next.HandoffPoints, _ = decodePoints(
				payload.LinkedPlacement.HandoffRoutePoints,
			)
			next.HandoffLength = routeLength(next.HandoffPoints)
			next.SecondaryPoints, _ = decodePoints(
				payload.SecondaryRoute.Points,
			)
			next.SecondaryLength = routeLength(next.SecondaryPoints)
			next.SecondaryHandoffPoints, _ = decodePoints(
				payload.SecondaryHandoff.Points,
			)
			next.SecondaryHandoffLength = routeLength(
				next.SecondaryHandoffPoints,
			)
			nextJourney.Operations = append(nextJourney.Operations, next)
		}
		compiled = append(compiled, nextJourney)
	}
	return compiled, nil
}

func compileActor(actor npcdata.Actor) (*compiledActor, error) {
	compiled := &compiledActor{
		Source:   actor,
		Variants: make(map[string]*compiledVariant),
	}
	variants := actor.ScheduleVariants
	if len(variants) == 0 {
		variantID := actor.ScheduleVariantID
		if variantID == "" {
			variantID = "default"
		}
		variants = []npcdata.ScheduleVariant{{
			ScheduleVariantID: variantID,
			SelectorIndices:   []int{1},
			Journeys:          actor.Journeys,
		}}
		compiled.Source.DefaultScheduleVariantID = variantID
	}
	for _, variant := range variants {
		journeys, err := compileJourneys(variant.Journeys)
		if err != nil {
			return nil, err
		}
		compiled.Variants[variant.ScheduleVariantID] = &compiledVariant{
			ID:       variant.ScheduleVariantID,
			Journeys: journeys,
		}
	}
	return compiled, nil
}

func (i *Interpreter) Compile(actors []npcdata.Actor) error {
	compiled := make(map[string]*compiledActor, len(actors))
	for _, actor := range actors {
		program, err := compileActor(actor)
		if err != nil {
			return err
		}
		compiled[actor.InstanceID] = program
	}
	i.mu.Lock()
	i.actors = compiled
	i.mu.Unlock()
	return nil
}
