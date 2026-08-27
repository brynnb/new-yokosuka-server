package protocol

import (
	"encoding/json"

	"github.com/brynnb/new-yokosuka-server/internal/dialoguestate"
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

const Version = 8

const (
	TypeWelcome                = "welcome"
	TypeSessionReplaced        = "session_replaced"
	TypePresence               = "presence"
	TypeSnapshot               = "snapshot"
	TypePlayerState            = "player_state"
	TypePlayerEnter            = "player_entered"
	TypePlayerLeft             = "player_left"
	TypeChat                   = "chat"
	TypeSystemMessage          = "system_message"
	TypeChatRejected           = "chat_rejected"
	TypeClientCount            = "client_count"
	TypePlayerDirectory        = "player_directory"
	TypePlayerDirectoryRequest = "player_directory_request"
	TypeLeaveWorld             = "leave_world"
	TypeWorldState             = "world_state"
	TypeForkliftUpdate         = "forklift_update"
	TypeForkliftState          = "forklift_state"
	TypeForkliftSound          = "forklift_sound"
	TypeForkliftRemoved        = "forklift_removed"
	TypeForkliftSpawn          = "forklift_spawn"
	TypeCargoClaim             = "cargo_claim"
	TypeCargoUpdate            = "cargo_update"
	TypeCargoState             = "cargo_state"
	TypeCargoRemoved           = "cargo_removed"
	TypeNPCState               = "npc_state"
	TypeNPCRemoved             = "npc_removed"
	TypeVendingPurchase        = "vending_purchase"
	TypeVendingResult          = "vending_result"
	TypeTransitionRequest      = "transition_request"
	TypeTransitionResult       = "transition_result"
	TypeTransitionCommit       = "transition_commit"
	TypeTransitionCommitResult = "transition_commit_result"
	TypeArcadeHighScore        = "arcade_high_score"
	TypeScriptEventStart       = "script_event_start"
	TypeScriptEventAdvance     = "script_event_advance"
	TypeScriptEventYield       = "script_event_yield"
	TypeScriptEventRejected    = "script_event_rejected"
	TypeClientDiagnostic       = "client_diagnostic"
)

type Header struct {
	Version int    `json:"v"`
	Type    string `json:"type"`
}

type Self struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccountID   int64  `json:"accountId,omitempty"`
	CharacterID int64  `json:"characterId,omitempty"`
	AccountType string `json:"accountType,omitempty"`
	AvatarID    string `json:"avatarId,omitempty"`
}

type CharacterProfile struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	AvatarID          string  `json:"avatarId"`
	WorldID           string  `json:"worldId"`
	X                 float64 `json:"x"`
	Y                 float64 `json:"y"`
	Z                 float64 `json:"z"`
	Yaw               float64 `json:"yaw"`
	Experience        int64   `json:"experience"`
	Level             int     `json:"level"`
	CurrentHP         int     `json:"currentHp"`
	MaxHP             int     `json:"maxHp"`
	Yen               int64   `json:"yen"`
	TimePlayedSeconds int64   `json:"timePlayedSeconds"`
}

type InventoryItem struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	MaxStack    int    `json:"maxStack"`
	Usable      bool   `json:"usable"`
	EffectKind  string `json:"effectKind,omitempty"`
	EffectValue int    `json:"effectValue,omitempty"`
	Quantity    int    `json:"quantity"`
}

type VendingPurchaseRequest struct {
	Header
	RequestID string `json:"requestId"`
	MachineID string `json:"machineId"`
	DrinkKey  string `json:"drinkKey"`
}

type VendingResult struct {
	Header
	RequestID  string          `json:"requestId"`
	MachineID  string          `json:"machineId,omitempty"`
	DrinkKey   string          `json:"drinkKey,omitempty"`
	DrinkName  string          `json:"drinkName,omitempty"`
	Price      int64           `json:"price,omitempty"`
	WinningCan bool            `json:"winningCan"`
	Outcome    string          `json:"outcome"`
	Message    string          `json:"message,omitempty"`
	Yen        int64           `json:"yen,omitempty"`
	Inventory  []InventoryItem `json:"inventory,omitempty"`
}

type ScriptEventStartRequest struct {
	Header
	RequestID string `json:"requestId"`
	Kind      string `json:"kind"`
	Actor     string `json:"actor,omitempty"`
	Object    string `json:"object,omitempty"`
	Activity  string `json:"activity,omitempty"`
}

type ScriptEventAdvanceRequest struct {
	Header
	RequestID string `json:"requestId"`
	RunID     int64  `json:"runId"`
	Action    string `json:"action"`
	OptionID  *int   `json:"optionId,omitempty"`
}

type ScriptEventYield struct {
	Header
	RequestID  string                          `json:"requestId"`
	RunID      int64                           `json:"runId"`
	ScriptID   int64                           `json:"scriptId"`
	VersionID  int64                           `json:"versionId"`
	ScriptSlug string                          `json:"scriptSlug"`
	Event      scriptruntime.Event             `json:"event"`
	Line       *scriptcontent.CompiledLine     `json:"line,omitempty"`
	Options    []scriptcontent.PresentedOption `json:"options,omitempty"`
	State      *store.CharacterScriptState     `json:"state,omitempty"`
}

type ScriptEventRejected struct {
	Header
	RequestID string `json:"requestId"`
	RunID     int64  `json:"runId,omitempty"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

type ClientDiagnostic struct {
	Header
	Scope   string          `json:"scope"`
	RunID   int64           `json:"runId,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

type TransitionRequest struct {
	Header
	RequestID          string `json:"requestId"`
	TransitionID       string `json:"transitionId"`
	DoorSelector       int    `json:"doorSelector"`
	SourceWorldID      string `json:"sourceWorldId"`
	DestinationWorldID string `json:"destinationWorldId"`
}

type TransitionResult struct {
	Header
	RequestID          string `json:"requestId"`
	TransitionID       string `json:"transitionId"`
	SourceWorldID      string `json:"sourceWorldId,omitempty"`
	DestinationWorldID string `json:"destinationWorldId,omitempty"`
	Authorized         bool   `json:"authorized"`
	AuthorizationID    string `json:"authorizationId,omitempty"`
	Reason             string `json:"reason"`
	Message            string `json:"message,omitempty"`
	ServerTimeMs       int64  `json:"serverTimeMs"`
	GameTimeMs         int64  `json:"gameTimeMs"`
	ExpiresAtMs        int64  `json:"expiresAtMs,omitempty"`
}

type TransitionCommit struct {
	Header
	RequestID       string `json:"requestId"`
	AuthorizationID string `json:"authorizationId"`
}

type TransitionCommitResult struct {
	Header
	RequestID          string `json:"requestId"`
	TransitionID       string `json:"transitionId,omitempty"`
	DestinationWorldID string `json:"destinationWorldId,omitempty"`
	Committed          bool   `json:"committed"`
	Reason             string `json:"reason"`
	Message            string `json:"message,omitempty"`
}

type WorldState struct {
	ServerTimeMs   int64   `json:"serverTimeMs"`
	GameTimeMs     int64   `json:"gameTimeMs"`
	EpochMs        int64   `json:"epochMs"`
	DayLengthMs    int64   `json:"dayLengthMs"`
	DayStartHour   float64 `json:"dayStartHour"`
	DayEndHour     float64 `json:"dayEndHour"`
	DayNumber      int64   `json:"dayNumber"`
	DayProgress    float64 `json:"dayProgress"`
	TimeOfDay      string  `json:"timeOfDay"`
	TimeOfDayIndex int     `json:"timeOfDayIndex"`
	Season         string  `json:"season"`
	SeasonIndex    int     `json:"seasonIndex"`
	Weather        string  `json:"weather"`
	WeatherIndex   int     `json:"weatherIndex"`
	Revision       uint64  `json:"revision"`
}

type Welcome struct {
	Header
	Self             Self                    `json:"self"`
	Character        *CharacterProfile       `json:"character,omitempty"`
	Inventory        []InventoryItem         `json:"inventory,omitempty"`
	DialogueState    *dialoguestate.Snapshot `json:"dialogueState,omitempty"`
	ChatHistory      []ChatMessage           `json:"chatHistory,omitempty"`
	WorldState       WorldState              `json:"worldState"`
	ConnectedClients int                     `json:"connectedClients"`
}

type SessionReplaced struct {
	Header
	Message string `json:"message"`
}

type ClientCount struct {
	Header
	ConnectedClients int `json:"connectedClients"`
}

type OnlinePlayer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	WorldID string `json:"worldId,omitempty"`
}

type PlayerDirectory struct {
	Header
	Players []OnlinePlayer `json:"players"`
}

type Presence struct {
	Header
	WorldID           string  `json:"worldId"`
	CharacterID       string  `json:"characterId,omitempty"`
	AvatarID          string  `json:"avatarId,omitempty"`
	X                 float64 `json:"x"`
	Y                 float64 `json:"y"`
	Z                 float64 `json:"z"`
	Yaw               float64 `json:"yaw"`
	Movement          string  `json:"movement"`
	AnimationID       *string `json:"animationId"`
	AnimationRevision uint64  `json:"animationRevision"`
	Sequence          uint64  `json:"sequence"`
	VehicleID         *string `json:"vehicleId"`
	VehicleLift       float64 `json:"vehicleLift"`
	VehicleSteering   float64 `json:"vehicleSteering"`
	VehicleWheelRoll  float64 `json:"vehicleWheelRoll"`
	VehicleQX         float64 `json:"vehicleQx"`
	VehicleQY         float64 `json:"vehicleQy"`
	VehicleQZ         float64 `json:"vehicleQz"`
	VehicleQW         float64 `json:"vehicleQw"`
}

type PlayerState struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	WorldID            string  `json:"worldId"`
	CharacterID        string  `json:"characterId"`
	AvatarID           string  `json:"avatarId"`
	X                  float64 `json:"x"`
	Y                  float64 `json:"y"`
	Z                  float64 `json:"z"`
	Yaw                float64 `json:"yaw"`
	Movement           string  `json:"movement"`
	AnimationID        *string `json:"animationId"`
	AnimationRevision  uint64  `json:"animationRevision"`
	AnimationElapsedMs int64   `json:"animationElapsedMs"`
	Sequence           uint64  `json:"sequence"`
	UpdatedAt          int64   `json:"updatedAt"`
	VehicleID          *string `json:"vehicleId"`
	VehicleLift        float64 `json:"vehicleLift"`
	VehicleSteering    float64 `json:"vehicleSteering"`
	VehicleWheelRoll   float64 `json:"vehicleWheelRoll"`
	VehicleQX          float64 `json:"vehicleQx"`
	VehicleQY          float64 `json:"vehicleQy"`
	VehicleQZ          float64 `json:"vehicleQz"`
	VehicleQW          float64 `json:"vehicleQw"`
}

type PlayerEntered struct {
	Header
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	WorldID  string `json:"worldId"`
}

type Snapshot struct {
	Header
	Players   []PlayerState   `json:"players"`
	Forklifts []ForkliftState `json:"forklifts"`
	Cargo     []CargoState    `json:"cargo"`
	NPCs      []NPCState      `json:"npcs"`
}

type NPCState struct {
	ID                   string         `json:"id"`
	ActorCode            string         `json:"actorCode"`
	Label                string         `json:"label"`
	WorldID              string         `json:"worldId"`
	X                    float64        `json:"x"`
	Y                    float64        `json:"y"`
	Z                    float64        `json:"z"`
	DirectionX           float64        `json:"directionX"`
	DirectionZ           float64        `json:"directionZ"`
	Yaw                  float64        `json:"yaw"`
	Mode                 string         `json:"mode"`
	Operation            int            `json:"operation"`
	OperationFileOffset  string         `json:"operationFileOffset,omitempty"`
	RouteID              string         `json:"routeId,omitempty"`
	RouteSegment         int            `json:"routeSegment"`
	RouteSegmentProgress float64        `json:"routeSegmentProgress"`
	RouteDistance        float64        `json:"routeDistance"`
	RouteLength          float64        `json:"routeLength"`
	SpeedPerGameSecond   float64        `json:"speedPerGameSecond"`
	MovementMode         string         `json:"movementMode,omitempty"`
	MotionStateID        int            `json:"motionStateId,omitempty"`
	ModelOverrideCode    string         `json:"modelOverrideCode,omitempty"`
	ScheduleVariantID    string         `json:"scheduleVariantId,omitempty"`
	Visual               NPCVisualState `json:"visual"`
	EffectiveSecond      float64        `json:"effectiveSecond"`
	AccumulatedDelay     float64        `json:"accumulatedDelay"`
	BlockedBy            string         `json:"blockedBy,omitempty"`
	Avoiding             bool           `json:"avoiding,omitempty"`
	AvoidancePhase       string         `json:"avoidancePhase,omitempty"`
	AvoidanceOffsetX     float64        `json:"avoidanceOffsetX,omitempty"`
	AvoidanceOffsetZ     float64        `json:"avoidanceOffsetZ,omitempty"`
	AvoidanceTargetX     float64        `json:"avoidanceTargetX,omitempty"`
	AvoidanceTargetZ     float64        `json:"avoidanceTargetZ,omitempty"`
	AvoidanceSpeed       float64        `json:"avoidanceSpeed,omitempty"`
	AvoidanceMotionTime  float64        `json:"avoidanceMotionTime,omitempty"`
	TransitionKind       string         `json:"transitionKind,omitempty"`
	TransitionDistance   float64        `json:"transitionDistance,omitempty"`
	Revision             uint64         `json:"revision"`
	UpdatedAt            int64          `json:"updatedAt"`
}

type NPCLocalObject struct {
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

type NPCSecondaryAttachment struct {
	ObjectCode           string    `json:"objectCode"`
	Area                 string    `json:"area"`
	Position             []float64 `json:"position"`
	RootYaw              float64   `json:"rootYaw"`
	TransformControlWord int       `json:"transformControlWord"`
	SourceOffset         string    `json:"sourceOffset,omitempty"`
}

type NPCVisualState struct {
	ActorLifecycleControlState *int                     `json:"actorLifecycleControlState,omitempty"`
	ActorQueryState            *int                     `json:"actorQueryState,omitempty"`
	ActorBooleanControllerMode *int                     `json:"actorBooleanControllerMode,omitempty"`
	ActorBoundsControlMode     *int                     `json:"actorBoundsControlMode,omitempty"`
	ActionControllerID         *int                     `json:"actionControllerId,omitempty"`
	ActionControllerMode       *int                     `json:"actionControllerMode,omitempty"`
	InteractionTargetCode      string                   `json:"interactionTargetCode,omitempty"`
	LocalObjects               []NPCLocalObject         `json:"localObjects"`
	SecondaryAttachments       []NPCSecondaryAttachment `json:"secondaryAttachments"`
	SecondaryObjectCode        string                   `json:"secondaryObjectCode,omitempty"`
}

type NPCStateEvent struct {
	Header
	NPC NPCState `json:"npc"`
}

type NPCRemoved struct {
	Header
	NPCID     string `json:"npcId"`
	WorldID   string `json:"worldId"`
	Revision  uint64 `json:"revision"`
	UpdatedAt int64  `json:"updatedAt"`
}

type ForkliftState struct {
	ID               string  `json:"id"`
	WorldID          string  `json:"worldId"`
	X                float64 `json:"x"`
	Y                float64 `json:"y"`
	Z                float64 `json:"z"`
	Yaw              float64 `json:"yaw"`
	QX               float64 `json:"qx"`
	QY               float64 `json:"qy"`
	QZ               float64 `json:"qz"`
	QW               float64 `json:"qw"`
	Lift             float64 `json:"lift"`
	Steering         float64 `json:"steering"`
	WheelRoll        float64 `json:"wheelRoll"`
	VelocityX        float64 `json:"velocityX"`
	VelocityY        float64 `json:"velocityY"`
	VelocityZ        float64 `json:"velocityZ"`
	AngularVelocityX float64 `json:"angularVelocityX"`
	AngularVelocityY float64 `json:"angularVelocityY"`
	AngularVelocityZ float64 `json:"angularVelocityZ"`
	OwnerID          string  `json:"ownerId"`
	RightingUntilMs  int64   `json:"rightingUntilMs"`
	ExpiresAtMs      int64   `json:"expiresAtMs"`
	UpdatedAtMs      int64   `json:"updatedAtMs"`
}

type ForkliftSpawn struct {
	Header
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	Z   float64 `json:"z"`
	Yaw float64 `json:"yaw"`
}

type ForkliftUpdate struct {
	Header
	ForkliftState
	Release  bool `json:"release"`
	Righting bool `json:"righting"`
}

type ForkliftStateEvent struct {
	Header
	Forklift ForkliftState `json:"forklift"`
}

type ForkliftSound struct {
	Header
	ForkliftID string `json:"forkliftId"`
	Cue        string `json:"cue"`
}

type ForkliftRemoved struct {
	Header
	ForkliftID string `json:"forkliftId"`
}

type CargoState struct {
	ID               string  `json:"id"`
	WorldID          string  `json:"worldId"`
	X                float64 `json:"x"`
	Y                float64 `json:"y"`
	Z                float64 `json:"z"`
	QX               float64 `json:"qx"`
	QY               float64 `json:"qy"`
	QZ               float64 `json:"qz"`
	QW               float64 `json:"qw"`
	VelocityX        float64 `json:"velocityX"`
	VelocityY        float64 `json:"velocityY"`
	VelocityZ        float64 `json:"velocityZ"`
	AngularVelocityX float64 `json:"angularVelocityX"`
	AngularVelocityY float64 `json:"angularVelocityY"`
	AngularVelocityZ float64 `json:"angularVelocityZ"`
	Sleeping         bool    `json:"sleeping"`
	AutoRight        bool    `json:"autoRight"`
	OwnerID          string  `json:"ownerId"`
	ClaimExpiresAtMs int64   `json:"claimExpiresAtMs"`
	UpdatedAtMs      int64   `json:"updatedAtMs"`
}

type CargoClaim struct {
	Header
	CargoID string `json:"cargoId"`
}

type CargoUpdate struct {
	Header
	CargoState
	Touching bool `json:"touching"`
}

type CargoStateEvent struct {
	Header
	Cargo CargoState `json:"cargo"`
}

type CargoRemoved struct {
	Header
	CargoID string `json:"cargoId"`
}

type PlayerStateEvent struct {
	Header
	Player PlayerState `json:"player"`
}

type PlayerLeft struct {
	Header
	PlayerID  string `json:"playerId"`
	UpdatedAt int64  `json:"updatedAt"`
}

type ChatRequest struct {
	Header
	Text string `json:"text"`
}

type ChatMessage struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	WorldID  string `json:"worldId"`
	Text     string `json:"text"`
	SentAt   int64  `json:"sentAt"`
}

type ChatEvent struct {
	Header
	Message ChatMessage `json:"message"`
}

type ChatRejected struct {
	Header
	Reason string `json:"reason"`
}

type SystemMessage struct {
	Header
	Text   string `json:"text"`
	SentAt int64  `json:"sentAt"`
}

type ArcadeHighScoreEvent struct {
	Header
	MachineID  string  `json:"machineId"`
	Score      float64 `json:"score"`
	PlayerName string  `json:"playerName"`
	AchievedAt int64   `json:"achievedAt"`
}

type LeaveWorld struct {
	Header
}

type WorldStateEvent struct {
	Header
	WorldState WorldState `json:"worldState"`
}

func NewHeader(messageType string) Header {
	return Header{Version: Version, Type: messageType}
}
