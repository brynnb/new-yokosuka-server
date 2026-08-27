package npc

import (
	"context"
	"time"
)

type Mode string
type AvoidancePhase string

const (
	ModeHidden  Mode = "hidden"
	ModeWalking Mode = "walking"
	ModeWaiting Mode = "waiting"
	ModeActing  Mode = "acting"
	ModeBlocked Mode = "blocked"

	AvoidanceNone      AvoidancePhase = ""
	AvoidanceSidestep  AvoidancePhase = "sidestep"
	AvoidancePassing   AvoidancePhase = "passing"
	AvoidanceReturning AvoidancePhase = "returning"
)

type State struct {
	ID                   string
	ActorCode            string
	Label                string
	WorldID              string
	Area                 string
	Position             Vector3
	HasPosition          bool
	Direction            Vector3
	Yaw                  float64
	Mode                 Mode
	Operation            int
	OperationFileOffset  string
	RouteID              string
	RouteSegment         int
	RouteSegmentProgress float64
	RouteDistance        float64
	RouteLength          float64
	SpeedPerGameSecond   float64
	MovementMode         string
	MotionStateID        int
	ModelOverrideCode    string
	ScheduleVariantID    string
	Visual               VisualState
	EffectiveSecond      float64
	AccumulatedDelay     float64
	BlockedBy            string
	Avoiding             bool
	AvoidancePhase       AvoidancePhase
	AvoidanceOffset      Vector3
	AvoidanceTarget      Vector3
	AvoidanceSpeed       float64
	AvoidanceMotionTime  float64
	TransitionKind       string
	TransitionDistance   float64
	Revision             uint64
	UpdatedAt            time.Time
}

type LocalObject struct {
	ObjectCode            string    `json:"objectCode"`
	LocationCode          string    `json:"locationCode"`
	Area                  string    `json:"area"`
	ControlWord           int       `json:"controlWord"`
	PlacementMode         int       `json:"placementMode"`
	RuntimePosition       []float64 `json:"runtimePosition"`
	WorldPosition         []float64 `json:"worldPosition"`
	TransformControlWords []int     `json:"transformControlWords"`
	ResolvedModel         string    `json:"resolvedModel,omitempty"`
	SourceOffset          string    `json:"sourceOffset,omitempty"`
}

type SecondaryAttachment struct {
	ObjectCode           string    `json:"objectCode"`
	Area                 string    `json:"area"`
	Position             []float64 `json:"position"`
	RootYaw              float64   `json:"rootYaw"`
	TransformControlWord int       `json:"transformControlWord"`
	SourceOffset         string    `json:"sourceOffset,omitempty"`
}

// VisualState contains rendering decisions produced by the authoritative
// interpreter. Asset lookup and animation playback remain browser concerns.
type VisualState struct {
	ActorLifecycleControlState *int                  `json:"actorLifecycleControlState,omitempty"`
	ActorQueryState            *int                  `json:"actorQueryState,omitempty"`
	ActorBooleanControllerMode *int                  `json:"actorBooleanControllerMode,omitempty"`
	ActorBoundsControlMode     *int                  `json:"actorBoundsControlMode,omitempty"`
	ActionControllerID         *int                  `json:"actionControllerId,omitempty"`
	ActionControllerMode       *int                  `json:"actionControllerMode,omitempty"`
	InteractionTargetCode      string                `json:"interactionTargetCode,omitempty"`
	LocalObjects               []LocalObject         `json:"localObjects"`
	SecondaryAttachments       []SecondaryAttachment `json:"secondaryAttachments"`
	SecondaryObjectCode        string                `json:"secondaryObjectCode,omitempty"`
}

func (state State) Visible() bool {
	return state.WorldID != "" && state.Mode != ModeHidden && state.HasPosition
}

type Player struct {
	ID       string
	WorldID  string
	Position Vector3
	Velocity Vector3
	Radius   float64
}

type Change struct {
	Previous State
	Current  State
}

type Checkpoint struct {
	NPCID            string
	DayNumber        int64
	AccumulatedDelay float64
	BlockedBy        string
	Revision         uint64
	UpdatedAt        time.Time
}

type CheckpointStore interface {
	LoadNPCCheckpoints(context.Context) ([]Checkpoint, error)
	SaveNPCCheckpoints(context.Context, []Checkpoint) error
}
