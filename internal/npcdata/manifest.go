package npcdata

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// manifestJSON is generated from the evidence-rich source actor catalog by
// tools/generate/build_server_npc_manifest.mjs. Keeping the runtime boundary
// here prevents the simulation package from depending on extraction details.
//
//go:embed manifest.json
var manifestJSON []byte

//go:embed transition-audit.json
var transitionAuditJSON []byte

type Manifest struct {
	Schema     string            `json:"schema"`
	AreaWorlds map[string]string `json:"areaWorlds"`
	Actors     []Actor           `json:"actors"`
}

type Actor struct {
	InstanceID     string          `json:"instanceId"`
	ActorCode      string          `json:"actorCode"`
	Label          string          `json:"label"`
	ModelCode      string          `json:"modelCode"`
	ModelOverrides json.RawMessage `json:"modelOverrides"`
	// NativeDefaultPathSpeedPerGameSecond is native distance per 30 Hz
	// controller update.
	NativeDefaultPathSpeedPerGameSecond float64           `json:"nativeDefaultPathSpeedPerGameSecond"`
	NativeDefaultMotionStateID          int               `json:"nativeDefaultMotionStateId"`
	DefaultArea                         string            `json:"defaultArea"`
	PlaybackWorldIDs                    []string          `json:"playbackWorldIds"`
	ScheduleSelector                    ScheduleSelector  `json:"scheduleSelector"`
	DefaultScheduleVariantID            string            `json:"defaultScheduleVariantId"`
	ScheduleVariants                    []ScheduleVariant `json:"scheduleVariants"`
	// Focused unit fixtures may provide one already-selected schedule.
	ScheduleVariantID string    `json:"scheduleVariantId,omitempty"`
	Journeys          []Journey `json:"journeys,omitempty"`
}

type ScheduleSelector struct {
	PointerSlots []SelectorSlot      `json:"pointerSlots"`
	Conditions   []ScheduleCondition `json:"conditions"`
}

type SelectorSlot struct {
	SelectorIndex      int    `json:"selectorIndex"`
	RawSchedulePointer string `json:"rawSchedulePointer"`
	ScheduleFileOffset string `json:"scheduleFileOffset"`
}

type ScheduleCondition struct {
	RequiredSetFlags     []int `json:"requiredSetFlags"`
	RequiredClearFlags   []int `json:"requiredClearFlags"`
	StartMonth           int   `json:"startMonth"`
	StartDay             int   `json:"startDay"`
	EndMonth             int   `json:"endMonth"`
	EndDay               int   `json:"endDay"`
	RequiredBaseSelector int   `json:"requiredBaseSelector"`
	TargetSelectorIndex  int   `json:"targetSelectorIndex"`
}

type ScheduleVariant struct {
	ScheduleVariantID string    `json:"scheduleVariantId"`
	SelectorIndices   []int     `json:"selectorIndices"`
	Journeys          []Journey `json:"journeys"`
}

type Journey struct {
	StartSecond int         `json:"startSecond"`
	StartTime   string      `json:"startTime"`
	Areas       []string    `json:"areas"`
	Operations  []Operation `json:"operations"`
}

// Operation intentionally retains its authored payload as JSON. The schedule
// interpreter decodes only the operation-specific fields it owns, while visual
// metadata can be forwarded without growing one universal data structure.
type Operation struct {
	Code       int             `json:"operation"`
	FileOffset string          `json:"fileOffset"`
	RouteID    string          `json:"routeId"`
	Payload    json.RawMessage `json:"-"`
}

type TransitionAudit struct {
	Schema  string `json:"schema"`
	Summary struct {
		TransitionCount  int            `json:"transitionCount"`
		VisibleWarpCount int            `json:"visibleWarpCount"`
		Classifications  map[string]int `json:"classifications"`
	} `json:"summary"`
	Transitions []Transition `json:"transitions"`
}

type Transition struct {
	ActorID            string  `json:"actorId"`
	ScheduleVariantID  string  `json:"scheduleVariantId"`
	JourneyStartSecond int     `json:"journeyStartSecond"`
	FromRouteID        string  `json:"fromRouteId"`
	ToRouteID          string  `json:"toRouteId"`
	Distance           float64 `json:"distance"`
	Classification     string  `json:"classification"`
	VisibleWarp        bool    `json:"visibleWarp"`
}

func (operation *Operation) UnmarshalJSON(data []byte) error {
	type fields Operation
	var decoded fields
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*operation = Operation(decoded)
	operation.Payload = append(operation.Payload[:0], data...)
	return nil
}

func Load() (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return nil, fmt.Errorf("decode embedded NPC manifest: %w", err)
	}
	if manifest.Schema != "new-yokosuka-server-npcs-v1" {
		return nil, fmt.Errorf("unsupported NPC manifest schema %q", manifest.Schema)
	}
	for _, actor := range manifest.Actors {
		variants := make(map[string]bool, len(actor.ScheduleVariants))
		for _, variant := range actor.ScheduleVariants {
			if variant.ScheduleVariantID == "" || variants[variant.ScheduleVariantID] {
				return nil, fmt.Errorf("invalid schedule variants for %s", actor.InstanceID)
			}
			variants[variant.ScheduleVariantID] = true
		}
		if !variants[actor.DefaultScheduleVariantID] {
			return nil, fmt.Errorf("missing default schedule variant for %s", actor.InstanceID)
		}
		if len(actor.ScheduleSelector.PointerSlots) != 16 {
			return nil, fmt.Errorf("%s has %d selector slots", actor.InstanceID, len(actor.ScheduleSelector.PointerSlots))
		}
	}
	return &manifest, nil
}

func LoadTransitionAudit() (*TransitionAudit, error) {
	var audit TransitionAudit
	if err := json.Unmarshal(transitionAuditJSON, &audit); err != nil {
		return nil, fmt.Errorf("decode embedded NPC transition audit: %w", err)
	}
	if audit.Schema != "new-yokosuka-npc-transition-audit-v1" {
		return nil, fmt.Errorf(
			"unsupported NPC transition audit schema %q",
			audit.Schema,
		)
	}
	return &audit, nil
}
