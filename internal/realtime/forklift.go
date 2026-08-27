package realtime

import (
	"math"
	"strings"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

const (
	defaultDynamicForkliftSpawnLimit = 1
	harborDynamicForkliftSpawnLimit  = 20
	raceDynamicForkliftSpawnLimit    = 20
)

var clientForkliftSoundCues = map[string]bool{
	"horn":        true,
	"flip_impact": true,
}

func dynamicForkliftSpawnLimit(worldID string) int {
	switch worldID {
	case "mfsy":
		return harborDynamicForkliftSpawnLimit
	case "ma00race":
		return raceDynamicForkliftSpawnLimit
	default:
		return defaultDynamicForkliftSpawnLimit
	}
}

func (h *Hub) dynamicForkliftCountLocked(worldID string) int {
	count := 0
	for id, state := range h.forklifts {
		if state.WorldID == worldID &&
			dynamicForkliftPattern.MatchString(id) {
			count++
		}
	}
	return count
}

func (h *Hub) scheduleForkliftExpiry(id string, expiresAtMs int64) {
	delay := time.Until(time.UnixMilli(expiresAtMs))
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		h.mu.Lock()
		state, exists := h.forklifts[id]
		if !exists || state.OwnerID != "" ||
			state.ExpiresAtMs != expiresAtMs {
			h.mu.Unlock()
			return
		}
		worldID := state.WorldID
		delete(h.forklifts, id)
		recipients := h.roomRecipientsLocked(worldID, "")
		h.mu.Unlock()
		h.sendMany(recipients, protocol.ForkliftRemoved{
			Header:     protocol.NewHeader(protocol.TypeForkliftRemoved),
			ForkliftID: id,
		})

		if _, baseForklift := validForklifts[id]; !baseForklift {
			return
		}
		time.AfterFunc(time.Second, func() {
			replacement, exists := initialForkliftStates()[id]
			if !exists {
				return
			}
			h.mu.Lock()
			if _, alreadyExists := h.forklifts[id]; alreadyExists {
				h.mu.Unlock()
				return
			}
			h.forklifts[id] = replacement
			recipients := h.roomRecipientsLocked(replacement.WorldID, "")
			h.mu.Unlock()
			h.sendMany(recipients, protocol.ForkliftStateEvent{
				Header:   protocol.NewHeader(protocol.TypeForkliftState),
				Forklift: replacement,
			})
		})
	})
}

func normalizeQuaternion(
	x float64,
	y float64,
	z float64,
	w float64,
) ([4]float64, bool) {
	values := [4]float64{x, y, z, w}
	lengthSquared := x*x + y*y + z*z + w*w
	if math.IsNaN(lengthSquared) || math.IsInf(lengthSquared, 0) ||
		lengthSquared < 0.25 || lengthSquared > 2.25 {
		return [4]float64{}, false
	}
	inverseLength := 1 / math.Sqrt(lengthSquared)
	for index := range values {
		values[index] *= inverseLength
	}
	return values, true
}

func normalizeForkliftOrientation(
	x float64,
	y float64,
	z float64,
	w float64,
	yaw float64,
) ([4]float64, bool) {
	if x == 0 && y == 0 && z == 0 && w == 0 {
		qx, qy, qz, qw := forkliftOrientationForYaw(yaw)
		return [4]float64{qx, qy, qz, qw}, true
	}
	return normalizeQuaternion(x, y, z, w)
}

func normalizeVehiclePresence(
	message protocol.Presence,
) (*string, [4]float64, bool) {
	if message.VehicleID == nil || strings.TrimSpace(*message.VehicleID) == "" {
		return nil, [4]float64{0, 0, 0, 1}, true
	}
	id := strings.TrimSpace(*message.VehicleID)
	if !validForkliftID(id) {
		return nil, [4]float64{}, false
	}
	if message.VehicleLift < 0 || message.VehicleLift > forkliftMaximumLift ||
		math.Abs(message.VehicleSteering) > 0.7 ||
		math.Abs(message.VehicleWheelRoll) > 1_000_000 {
		return nil, [4]float64{}, false
	}
	orientation, ok := normalizeForkliftOrientation(
		message.VehicleQX,
		message.VehicleQY,
		message.VehicleQZ,
		message.VehicleQW,
		message.Yaw,
	)
	if !ok {
		return nil, [4]float64{}, false
	}
	return &id, orientation, true
}

func (h *Hub) releaseForkliftLocked(playerID string, vehicleID *string) {
	if vehicleID == nil {
		return
	}
	if h.forkliftOwners[*vehicleID] == playerID {
		delete(h.forkliftOwners, *vehicleID)
	}
}

func (h *Hub) HandleForkliftSound(
	client *Client,
	message protocol.ForkliftSound,
) bool {
	if !validForkliftID(message.ForkliftID) ||
		!clientForkliftSoundCues[message.Cue] {
		return false
	}
	h.mu.RLock()
	presence, present := h.presences[client.id]
	state, exists := h.forklifts[message.ForkliftID]
	owned := present &&
		presence.state.VehicleID != nil &&
		*presence.state.VehicleID == message.ForkliftID &&
		h.forkliftOwners[message.ForkliftID] == client.id &&
		state.OwnerID == client.id &&
		state.WorldID == presence.state.WorldID
	var recipients []*Client
	if exists && owned {
		recipients = h.roomRecipientsLocked(state.WorldID, client.id)
	}
	h.mu.RUnlock()
	if !exists || !owned {
		return false
	}
	h.sendMany(recipients, protocol.ForkliftSound{
		Header:     protocol.NewHeader(protocol.TypeForkliftSound),
		ForkliftID: message.ForkliftID,
		Cue:        message.Cue,
	})
	return true
}

func (h *Hub) HandleForkliftUpdate(
	client *Client,
	message protocol.ForkliftUpdate,
) bool {
	if !validForkliftID(message.ID) {
		return false
	}
	numbers := []float64{
		message.X,
		message.Y,
		message.Z,
		message.Yaw,
		message.Lift,
		message.Steering,
		message.WheelRoll,
		message.VelocityX,
		message.VelocityY,
		message.VelocityZ,
		message.AngularVelocityX,
		message.AngularVelocityY,
		message.AngularVelocityZ,
		message.QX,
		message.QY,
		message.QZ,
		message.QW,
	}
	for _, value := range numbers {
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			math.Abs(value) > 1_000_000 {
			return false
		}
	}
	if message.Lift < 0 || message.Lift > forkliftMaximumLift ||
		math.Abs(message.Steering) > 0.7 {
		return false
	}
	orientation, ok := normalizeForkliftOrientation(
		message.QX,
		message.QY,
		message.QZ,
		message.QW,
		message.Yaw,
	)
	if !ok {
		return false
	}

	nowTime := time.Now()
	now := nowTime.UnixMilli()
	h.mu.Lock()
	presence, inWorld := h.presences[client.id]
	state, exists := h.forklifts[message.ID]
	if !inWorld || !exists || state.WorldID != presence.state.WorldID {
		h.mu.Unlock()
		return false
	}
	owner := h.forkliftOwners[message.ID]
	if message.Release {
		if owner != client.id {
			h.mu.Unlock()
			return false
		}
		delete(h.forkliftOwners, message.ID)
	} else if owner != "" && owner != client.id {
		h.mu.Unlock()
		return false
	}
	state = message.ForkliftState
	state.QX = orientation[0]
	state.QY = orientation[1]
	state.QZ = orientation[2]
	state.QW = orientation[3]
	state.ID = message.ID
	state.WorldID = presence.state.WorldID
	state.OwnerID = ""
	if !message.Release && owner == client.id {
		state.OwnerID = client.id
	}
	state.RightingUntilMs = 0
	if message.Righting {
		state.RightingUntilMs = now + 900
	}
	state.ExpiresAtMs = 0
	if message.Release {
		state.ExpiresAtMs = forkliftReleaseExpiry(state.WorldID, nowTime)
	}
	state.UpdatedAtMs = now
	h.forklifts[state.ID] = state
	recipients := h.roomRecipientsLocked(state.WorldID, "")
	h.mu.Unlock()

	h.sendMany(recipients, protocol.ForkliftStateEvent{
		Header:   protocol.NewHeader(protocol.TypeForkliftState),
		Forklift: state,
	})
	if state.ExpiresAtMs > 0 {
		h.scheduleForkliftExpiry(state.ID, state.ExpiresAtMs)
	}
	return true
}

func (h *Hub) HandleForkliftSpawn(
	client *Client,
	message protocol.ForkliftSpawn,
) bool {
	numbers := []float64{message.X, message.Y, message.Z, message.Yaw}
	for _, value := range numbers {
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			math.Abs(value) > 1_000_000 {
			return false
		}
	}
	rawID, err := randomHex(6)
	if err != nil {
		return false
	}
	now := time.Now().UnixMilli()
	h.mu.Lock()
	presence, inWorld := h.presences[client.id]
	if !inWorld {
		h.mu.Unlock()
		return false
	}
	worldID := presence.state.WorldID
	if h.dynamicForkliftCountLocked(worldID) >=
		dynamicForkliftSpawnLimit(worldID) {
		h.mu.Unlock()
		return false
	}
	state := protocol.ForkliftState{
		ID:      "forklift-" + rawID,
		WorldID: worldID,
		X:       message.X,
		Y:       message.Y,
		Z:       message.Z,
		Yaw:     message.Yaw,
		ExpiresAtMs: time.Now().Add(
			abandonedForkliftLifetime,
		).UnixMilli(),
		UpdatedAtMs: now,
	}
	state.QX, state.QY, state.QZ, state.QW = forkliftOrientationForYaw(
		message.Yaw,
	)
	h.forklifts[state.ID] = state
	recipients := h.roomRecipientsLocked(state.WorldID, "")
	h.mu.Unlock()
	h.sendMany(recipients, protocol.ForkliftStateEvent{
		Header:   protocol.NewHeader(protocol.TypeForkliftState),
		Forklift: state,
	})
	h.scheduleForkliftExpiry(state.ID, state.ExpiresAtMs)
	return true
}
