package realtime

import (
	"context"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npc"
	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

const npcSimulationInterval = 100 * time.Millisecond

func (h *Hub) SetNPCEngine(engine *npc.Engine) {
	h.mu.Lock()
	h.npcs = engine
	h.mu.Unlock()
}

func (h *Hub) RunNPCSimulation(ctx context.Context) {
	ticker := time.NewTicker(npcSimulationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			h.tickNPCs(now)
		}
	}
}

func (h *Hub) tickNPCs(_ time.Time) {
	h.mu.RLock()
	engine := h.npcs
	players := make([]npc.Player, 0, len(h.presences))
	for _, presence := range h.presences {
		state := presence.state
		radius := 0.45
		if state.VehicleID != nil {
			radius = 1.25
		}
		players = append(players, npc.Player{
			ID:       state.ID,
			WorldID:  state.WorldID,
			Position: npc.Vector3{X: state.X, Y: state.Y, Z: state.Z},
			Radius:   radius,
		})
	}
	h.mu.RUnlock()
	if engine == nil {
		return
	}
	world := h.world.Snapshot()
	// Position and timestamp must describe the same instant. A time.Ticker
	// value is the scheduled tick time and can be older than the world-clock
	// snapshot by the time a busy simulation processes it. Stamping a position
	// computed from the newer world time with that older value makes clients
	// extrapolate too far, then snap back at each correction broadcast.
	now := time.UnixMilli(world.ServerTimeMs)
	changes, err := engine.Tick(npc.TickTime{
		ServerTime: now,
		GameTime:   time.UnixMilli(world.GameTimeMs),
		DayNumber:  world.DayNumber,
		DayLength:  time.Duration(world.DayLengthMs) * time.Millisecond,
	}, players)
	if err != nil {
		h.logf("NPC simulation tick failed: %v", err)
		return
	}
	for _, change := range changes {
		h.publishNPCChange(change)
	}
}

// ResetNPCSchedules immediately reevaluates every NPC against the current
// authoritative world time and broadcasts the resulting clean state changes.
func (h *Hub) ResetNPCSchedules() {
	h.mu.RLock()
	engine := h.npcs
	h.mu.RUnlock()
	if engine == nil {
		return
	}
	engine.ResetScheduleState()
	h.tickNPCs(time.Now())
}

func (h *Hub) publishNPCChange(change npc.Change) {
	previous := change.Previous
	current := change.Current
	if previous.Visible() &&
		(!current.Visible() || previous.WorldID != current.WorldID) {
		h.mu.RLock()
		recipients := h.roomRecipientsLocked(previous.WorldID, "")
		h.mu.RUnlock()
		h.sendMany(recipients, protocol.NPCRemoved{
			Header:    protocol.NewHeader(protocol.TypeNPCRemoved),
			NPCID:     previous.ID,
			WorldID:   previous.WorldID,
			Revision:  current.Revision,
			UpdatedAt: current.UpdatedAt.UnixMilli(),
		})
	}
	if !current.Visible() {
		return
	}
	h.mu.RLock()
	recipients := h.roomRecipientsLocked(current.WorldID, "")
	h.mu.RUnlock()
	h.sendMany(recipients, protocol.NPCStateEvent{
		Header: protocol.NewHeader(protocol.TypeNPCState),
		NPC:    npcProtocolState(current),
	})
}

func npcProtocolState(state npc.State) protocol.NPCState {
	localObjects := make([]protocol.NPCLocalObject, 0, len(state.Visual.LocalObjects))
	for _, object := range state.Visual.LocalObjects {
		localObjects = append(localObjects, protocol.NPCLocalObject{
			ObjectCode:            object.ObjectCode,
			LocationCode:          object.LocationCode,
			Area:                  object.Area,
			ControlWord:           object.ControlWord,
			PlacementMode:         object.PlacementMode,
			RuntimePosition:       object.RuntimePosition,
			WorldPosition:         object.WorldPosition,
			TransformControlWords: object.TransformControlWords,
			ResolvedModel:         object.ResolvedModel,
			SourceOffset:          object.SourceOffset,
		})
	}
	attachments := make(
		[]protocol.NPCSecondaryAttachment,
		0,
		len(state.Visual.SecondaryAttachments),
	)
	for _, attachment := range state.Visual.SecondaryAttachments {
		attachments = append(attachments, protocol.NPCSecondaryAttachment{
			ObjectCode:           attachment.ObjectCode,
			Area:                 attachment.Area,
			Position:             attachment.Position,
			RootYaw:              attachment.RootYaw,
			TransformControlWord: attachment.TransformControlWord,
			SourceOffset:         attachment.SourceOffset,
		})
	}
	return protocol.NPCState{
		ID:                   state.ID,
		ActorCode:            state.ActorCode,
		Label:                state.Label,
		WorldID:              state.WorldID,
		X:                    state.Position.X,
		Y:                    state.Position.Y,
		Z:                    state.Position.Z,
		DirectionX:           state.Direction.X,
		DirectionZ:           state.Direction.Z,
		Yaw:                  state.Yaw,
		Mode:                 string(state.Mode),
		Operation:            state.Operation,
		OperationFileOffset:  state.OperationFileOffset,
		RouteID:              state.RouteID,
		RouteSegment:         state.RouteSegment,
		RouteSegmentProgress: state.RouteSegmentProgress,
		RouteDistance:        state.RouteDistance,
		RouteLength:          state.RouteLength,
		SpeedPerGameSecond:   state.SpeedPerGameSecond,
		MovementMode:         state.MovementMode,
		MotionStateID:        state.MotionStateID,
		ScheduleVariantID:    state.ScheduleVariantID,
		ModelOverrideCode:    state.ModelOverrideCode,
		Visual: protocol.NPCVisualState{
			ActorLifecycleControlState: state.Visual.ActorLifecycleControlState,
			ActorQueryState:            state.Visual.ActorQueryState,
			ActorBooleanControllerMode: state.Visual.ActorBooleanControllerMode,
			ActorBoundsControlMode:     state.Visual.ActorBoundsControlMode,
			ActionControllerID:         state.Visual.ActionControllerID,
			ActionControllerMode:       state.Visual.ActionControllerMode,
			InteractionTargetCode:      state.Visual.InteractionTargetCode,
			LocalObjects:               localObjects,
			SecondaryAttachments:       attachments,
			SecondaryObjectCode:        state.Visual.SecondaryObjectCode,
		},
		EffectiveSecond:     state.EffectiveSecond,
		AccumulatedDelay:    state.AccumulatedDelay,
		BlockedBy:           state.BlockedBy,
		Avoiding:            state.Avoiding,
		AvoidancePhase:      string(state.AvoidancePhase),
		AvoidanceOffsetX:    state.AvoidanceOffset.X,
		AvoidanceOffsetZ:    state.AvoidanceOffset.Z,
		AvoidanceTargetX:    state.AvoidanceTarget.X,
		AvoidanceTargetZ:    state.AvoidanceTarget.Z,
		AvoidanceSpeed:      state.AvoidanceSpeed,
		AvoidanceMotionTime: state.AvoidanceMotionTime,
		TransitionKind:      state.TransitionKind,
		TransitionDistance:  state.TransitionDistance,
		Revision:            state.Revision,
		UpdatedAt:           state.UpdatedAt.UnixMilli(),
	}
}

func (h *Hub) npcSnapshot(worldID string) []protocol.NPCState {
	h.mu.RLock()
	engine := h.npcs
	h.mu.RUnlock()
	if engine == nil {
		return []protocol.NPCState{}
	}
	states := engine.Snapshot(worldID)
	result := make([]protocol.NPCState, 0, len(states))
	for _, state := range states {
		result = append(result, npcProtocolState(state))
	}
	return result
}
