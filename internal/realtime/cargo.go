package realtime

import (
	"fmt"
	"math"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

func (h *Hub) markClientCargoForReleaseLocked(
	playerID string,
	now time.Time,
) []protocol.CargoState {
	var released []protocol.CargoState
	expiresAtMs := now.Add(cargoClaimReleaseDelay).UnixMilli()
	untouchedSince := time.UnixMilli(now.UnixMilli())
	for id, state := range h.cargo {
		if state.OwnerID != playerID {
			continue
		}
		state.ClaimExpiresAtMs = expiresAtMs
		state.UpdatedAtMs = untouchedSince.UnixMilli()
		h.cargo[id] = state
		h.cargoUntouched[id] = untouchedSince
		h.cargoLastTouched[id] = untouchedSince
		released = append(released, state)
	}
	return released
}

func (h *Hub) scheduleCargoClaimRelease(
	id string,
	ownerID string,
	expiresAtMs int64,
) {
	if ownerID == "" || expiresAtMs <= 0 {
		return
	}
	delay := time.Until(time.UnixMilli(expiresAtMs))
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		h.mu.Lock()
		state, exists := h.cargo[id]
		if !exists ||
			state.OwnerID != ownerID ||
			state.ClaimExpiresAtMs != expiresAtMs {
			h.mu.Unlock()
			return
		}
		if presence, connected := h.presences[ownerID]; connected &&
			presence.state.WorldID == state.WorldID {
			h.mu.Unlock()
			return
		}
		state.OwnerID = ""
		state.ClaimExpiresAtMs = 0
		state.UpdatedAtMs = time.Now().UnixMilli()
		h.cargo[id] = state
		recipients := h.roomRecipientsLocked(state.WorldID, "")
		h.mu.Unlock()
		h.sendMany(recipients, protocol.CargoStateEvent{
			Header: protocol.NewHeader(protocol.TypeCargoState),
			Cargo:  state,
		})
	})
}

func (h *Hub) cargoSpawnForState(
	state protocol.CargoState,
) cargoSpawnPosition {
	if spawn, exists := h.cargoSpawns[state.ID]; exists {
		return spawn
	}
	x, y, z := cargoSpawnForWorld(state.WorldID)
	return cargoSpawnPosition{x: x, y: y, z: z}
}

func (h *Hub) cargoOutsideSpawn(state protocol.CargoState) bool {
	spawn := h.cargoSpawnForState(state)
	dx := state.X - spawn.x
	dy := state.Y - spawn.y
	dz := state.Z - spawn.z
	return dx*dx+dy*dy+dz*dz > cargoSpawnRadius*cargoSpawnRadius
}

func cargoBottomFacesDown(state protocol.CargoState) bool {
	upDot := 1 - 2*(state.QX*state.QX+state.QZ*state.QZ)
	return upDot >= cargoUprightDotThreshold
}

func uprightCargoOrientation(state protocol.CargoState) (float64, float64) {
	yaw := math.Atan2(
		2*(state.QW*state.QY+state.QX*state.QZ),
		1-2*(state.QY*state.QY+state.QZ*state.QZ),
	)
	return math.Sin(yaw / 2), math.Cos(yaw / 2)
}

func (h *Hub) updateCargoAutoRightLocked(
	state protocol.CargoState,
	touching bool,
	now time.Time,
) (time.Time, bool) {
	if touching {
		delete(h.cargoUntouched, state.ID)
		return time.Time{}, false
	}
	if since, exists := h.cargoUntouched[state.ID]; exists {
		return since, false
	}
	h.cargoUntouched[state.ID] = now
	return now, true
}

func (h *Hub) scheduleCargoAutoRight(id string, untouchedSince time.Time) {
	time.AfterFunc(cargoAutoRightDelay, func() {
		now := time.Now()
		h.mu.Lock()
		state, recipients, changed := h.autoRightCargoLocked(
			id,
			untouchedSince,
			now,
		)
		h.mu.Unlock()
		if !changed {
			return
		}
		h.sendMany(recipients, protocol.CargoStateEvent{
			Header: protocol.NewHeader(protocol.TypeCargoState),
			Cargo:  state,
		})
	})
}

func (h *Hub) autoRightCargoLocked(
	id string,
	untouchedSince time.Time,
	now time.Time,
) (protocol.CargoState, []*Client, bool) {
	state, exists := h.cargo[id]
	currentSince, stillUntouched := h.cargoUntouched[id]
	if !exists ||
		!stillUntouched ||
		!currentSince.Equal(untouchedSince) ||
		now.Sub(currentSince) < cargoAutoRightDelay {
		return protocol.CargoState{}, nil, false
	}
	delete(h.cargoUntouched, id)
	state.OwnerID = ""
	state.ClaimExpiresAtMs = 0
	state.AutoRight = true
	if !cargoBottomFacesDown(state) {
		state.QY, state.QW = uprightCargoOrientation(state)
		state.QX = 0
		state.QZ = 0
	}
	state.VelocityX = 0
	state.VelocityY = 0
	state.VelocityZ = 0
	state.AngularVelocityX = 0
	state.AngularVelocityY = 0
	state.AngularVelocityZ = 0
	state.Sleeping = true
	state.UpdatedAtMs = now.UnixMilli()
	h.cargo[id] = state
	recipients := h.roomRecipientsLocked(state.WorldID, "")
	return state, recipients, true
}

func (h *Hub) scheduleCargoCleanup(id string, lastTouched time.Time) {
	delay := time.Until(lastTouched.Add(cargoCleanupAge))
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		now := time.Now()
		h.mu.Lock()
		worldID, recipients, removed := h.cleanupStaleCargoLocked(
			id,
			lastTouched,
			now,
		)
		h.mu.Unlock()
		if !removed {
			return
		}
		h.sendMany(recipients, protocol.CargoRemoved{
			Header:  protocol.NewHeader(protocol.TypeCargoRemoved),
			CargoID: id,
		})
		h.logf(
			"removed stale cargo %s from %s after %s untouched",
			id,
			worldID,
			cargoCleanupAge,
		)
	})
}

func (h *Hub) cleanupStaleCargoLocked(
	id string,
	expectedLastTouched time.Time,
	now time.Time,
) (string, []*Client, bool) {
	state, exists := h.cargo[id]
	lastTouched, tracked := h.cargoLastTouched[id]
	if !exists ||
		!tracked ||
		!lastTouched.Equal(expectedLastTouched) ||
		state.OwnerID != "" ||
		now.Sub(lastTouched) < cargoCleanupAge {
		return "", nil, false
	}
	worldCargoCount := 0
	for _, candidate := range h.cargo {
		if candidate.WorldID == state.WorldID {
			worldCargoCount++
		}
	}
	if worldCargoCount <= cargoCleanupThreshold {
		return "", nil, false
	}
	delete(h.cargo, id)
	delete(h.cargoSpawns, id)
	delete(h.cargoAwaySince, id)
	delete(h.cargoUntouched, id)
	delete(h.cargoLastTouched, id)
	delete(h.cargoReplaced, id)
	return state.WorldID, h.roomRecipientsLocked(state.WorldID, ""), true
}

func (h *Hub) updateCargoReplenishmentLocked(
	state protocol.CargoState,
	now time.Time,
) (time.Time, bool) {
	if h.cargoReplaced[state.ID] {
		delete(h.cargoAwaySince, state.ID)
		return time.Time{}, false
	}
	if !h.cargoOutsideSpawn(state) {
		delete(h.cargoAwaySince, state.ID)
		return time.Time{}, false
	}
	if since, exists := h.cargoAwaySince[state.ID]; exists {
		return since, false
	}
	h.cargoAwaySince[state.ID] = now
	return now, true
}

func (h *Hub) scheduleCargoReplenishment(
	sourceID string,
	awaySince time.Time,
) {
	time.AfterFunc(cargoReplenishDelay, func() {
		now := time.Now()
		h.mu.Lock()
		replacement, recipients, spawned := h.replenishCargoLocked(
			sourceID,
			awaySince,
			now,
		)
		h.mu.Unlock()
		if !spawned {
			return
		}
		h.sendMany(recipients, protocol.CargoStateEvent{
			Header: protocol.NewHeader(protocol.TypeCargoState),
			Cargo:  replacement,
		})
		h.scheduleCargoCleanup(replacement.ID, now)
	})
}

func (h *Hub) replenishCargoLocked(
	sourceID string,
	awaySince time.Time,
	now time.Time,
) (protocol.CargoState, []*Client, bool) {
	source, exists := h.cargo[sourceID]
	currentSince, stillAway := h.cargoAwaySince[sourceID]
	if !exists ||
		!stillAway ||
		!currentSince.Equal(awaySince) ||
		h.cargoReplaced[sourceID] ||
		now.Sub(currentSince) < cargoReplenishDelay ||
		!h.cargoOutsideSpawn(source) {
		return protocol.CargoState{}, nil, false
	}
	spawn := h.cargoSpawnForState(source)
	var id string
	for {
		id = fmt.Sprintf("cargo-job-%d", h.nextCargoID)
		h.nextCargoID++
		if _, collision := h.cargo[id]; !collision {
			break
		}
	}
	replacement := protocol.CargoState{
		ID:          id,
		WorldID:     source.WorldID,
		QW:          1,
		Sleeping:    true,
		UpdatedAtMs: now.UnixMilli(),
	}
	replacement.X, replacement.Y, replacement.Z = spawn.x, spawn.y, spawn.z
	h.cargo[id] = replacement
	h.cargoSpawns[id] = spawn
	h.cargoLastTouched[id] = now
	h.cargoReplaced[sourceID] = true
	delete(h.cargoAwaySince, sourceID)
	recipients := h.roomRecipientsLocked(replacement.WorldID, "")
	return replacement, recipients, true
}

func (h *Hub) HandleCargoClaim(
	client *Client,
	message protocol.CargoClaim,
) bool {
	if !cargoIDPattern.MatchString(message.CargoID) {
		return false
	}
	now := time.Now()
	h.mu.Lock()
	presence, inWorld := h.presences[client.id]
	state, exists := h.cargo[message.CargoID]
	if !inWorld || !exists ||
		state.WorldID != presence.state.WorldID ||
		presence.state.VehicleID == nil {
		h.mu.Unlock()
		return false
	}
	distanceSquared := (presence.state.X-state.X)*(presence.state.X-state.X) +
		(presence.state.Y-state.Y)*(presence.state.Y-state.Y) +
		(presence.state.Z-state.Z)*(presence.state.Z-state.Z)
	if distanceSquared > 36 {
		h.mu.Unlock()
		return false
	}
	if state.OwnerID != "" &&
		state.ClaimExpiresAtMs > 0 &&
		state.ClaimExpiresAtMs <= now.UnixMilli() {
		state.OwnerID = ""
		state.ClaimExpiresAtMs = 0
	}
	if state.OwnerID != "" && state.OwnerID != client.id {
		h.mu.Unlock()
		return false
	}
	state.OwnerID = client.id
	state.ClaimExpiresAtMs = 0
	state.AutoRight = false
	state.UpdatedAtMs = now.UnixMilli()
	h.cargo[state.ID] = state
	delete(h.cargoUntouched, state.ID)
	h.cargoLastTouched[state.ID] = now
	recipients := h.roomRecipientsLocked(state.WorldID, "")
	h.mu.Unlock()
	h.sendMany(recipients, protocol.CargoStateEvent{
		Header: protocol.NewHeader(protocol.TypeCargoState),
		Cargo:  state,
	})
	return true
}

func (h *Hub) HandleCargoUpdate(
	client *Client,
	message protocol.CargoUpdate,
) bool {
	if !cargoIDPattern.MatchString(message.ID) {
		return false
	}
	numbers := []float64{
		message.X, message.Y, message.Z,
		message.QX, message.QY, message.QZ, message.QW,
		message.VelocityX, message.VelocityY, message.VelocityZ,
		message.AngularVelocityX,
		message.AngularVelocityY,
		message.AngularVelocityZ,
	}
	for _, value := range numbers {
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			math.Abs(value) > 1_000_000 {
			return false
		}
	}
	if math.Hypot(
		math.Hypot(message.VelocityX, message.VelocityY),
		message.VelocityZ,
	) > 100 ||
		math.Hypot(
			math.Hypot(
				message.AngularVelocityX,
				message.AngularVelocityY,
			),
			message.AngularVelocityZ,
		) > 100 {
		return false
	}
	orientation, ok := normalizeQuaternion(
		message.QX,
		message.QY,
		message.QZ,
		message.QW,
	)
	if !ok {
		return false
	}
	now := time.Now()
	h.mu.Lock()
	presence, inWorld := h.presences[client.id]
	previous, exists := h.cargo[message.ID]
	if !inWorld || !exists ||
		previous.WorldID != presence.state.WorldID ||
		previous.OwnerID != client.id {
		h.mu.Unlock()
		return false
	}
	state := message.CargoState
	state.ID = previous.ID
	state.WorldID = previous.WorldID
	state.QX = orientation[0]
	state.QY = orientation[1]
	state.QZ = orientation[2]
	state.QW = orientation[3]
	state.OwnerID = client.id
	state.AutoRight = false
	if message.Touching {
		state.ClaimExpiresAtMs = 0
	} else if previous.ClaimExpiresAtMs > 0 {
		state.ClaimExpiresAtMs = previous.ClaimExpiresAtMs
	} else {
		state.ClaimExpiresAtMs = now.Add(
			cargoClaimReleaseDelay,
		).UnixMilli()
	}
	state.UpdatedAtMs = now.UnixMilli()
	h.cargo[state.ID] = state
	if message.Touching {
		h.cargoLastTouched[state.ID] = now
	} else if _, tracked := h.cargoLastTouched[state.ID]; !tracked {
		h.cargoLastTouched[state.ID] = now
	}
	untouchedSince, startAutoRight := h.updateCargoAutoRightLocked(
		state,
		message.Touching,
		now,
	)
	awaySince, startReplenishment := h.updateCargoReplenishmentLocked(
		state,
		now,
	)
	cleanupLastTouched := h.cargoLastTouched[state.ID]
	recipients := h.roomRecipientsLocked(state.WorldID, "")
	h.mu.Unlock()
	h.sendMany(recipients, protocol.CargoStateEvent{
		Header: protocol.NewHeader(protocol.TypeCargoState),
		Cargo:  state,
	})
	if !message.Touching {
		h.scheduleCargoClaimRelease(
			state.ID,
			state.OwnerID,
			state.ClaimExpiresAtMs,
		)
	}
	if startReplenishment {
		h.scheduleCargoReplenishment(state.ID, awaySince)
	}
	if startAutoRight {
		h.scheduleCargoAutoRight(state.ID, untouchedSince)
		h.scheduleCargoCleanup(
			state.ID,
			cleanupLastTouched,
		)
	}
	return true
}

func equalStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
