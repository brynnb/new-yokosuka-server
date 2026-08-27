package realtime

import (
	"strings"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/travelaccess"
)

const (
	transitionReasonAuthorized       = "authorized"
	transitionReasonCommitted        = "committed"
	transitionReasonInvalidRequest   = "invalid_request"
	transitionReasonUnknown          = "unknown_transition"
	transitionReasonSourceMismatch   = "source_mismatch"
	transitionReasonDoorMismatch     = "door_mismatch"
	transitionReasonDestination      = "destination_mismatch"
	transitionReasonNoPresence       = "no_presence"
	transitionReasonNotOnFoot        = "not_on_foot"
	transitionReasonTooFar           = "too_far"
	transitionReasonClosed           = "closed"
	transitionReasonExpired          = "authorization_expired"
	transitionReasonInvalidGrant     = "invalid_authorization"
	transitionReasonConnectionClosed = "connection_closed"
)

type transitionGrant struct {
	requestID       string
	authorizationID string
	rule            travelaccess.Rule
	expiresAt       time.Time
}

type authorizedArrival struct {
	sourceWorldID      string
	destinationWorldID string
}

func transitionRequestIDValid(requestID string) bool {
	trimmed := strings.TrimSpace(requestID)
	return trimmed == requestID && len(trimmed) > 0 && len(trimmed) <= 128
}

func (h *Hub) HandleTransitionRequest(
	client *Client,
	request protocol.TransitionRequest,
) bool {
	snapshot := h.world.Snapshot()
	result := protocol.TransitionResult{
		Header:             protocol.NewHeader(protocol.TypeTransitionResult),
		RequestID:          request.RequestID,
		TransitionID:       request.TransitionID,
		SourceWorldID:      request.SourceWorldID,
		DestinationWorldID: request.DestinationWorldID,
		Reason:             transitionReasonInvalidRequest,
		Message:            "This entrance request was invalid.",
		ServerTimeMs:       snapshot.ServerTimeMs,
		GameTimeMs:         snapshot.GameTimeMs,
	}

	h.mu.Lock()
	if h.clients[client.id] != client {
		h.mu.Unlock()
		return false
	}
	// Every new attempt invalidates an older unused grant on this connection.
	delete(h.transitionGrants, client.id)
	if !transitionRequestIDValid(request.RequestID) {
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	rule, known := h.travelAccess.Rule(request.TransitionID)
	if !known {
		result.Reason = transitionReasonUnknown
		result.Message = "This entrance is not recognized by the server."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if request.SourceWorldID != rule.Source.WorldID {
		result.Reason = transitionReasonSourceMismatch
		result.Message = "You are not at this entrance."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if request.DoorSelector != rule.Source.DoorSelector {
		result.Reason = transitionReasonDoorMismatch
		result.Message = "This door does not match the requested entrance."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if request.DestinationWorldID != rule.Destination.WorldID {
		result.Reason = transitionReasonDestination
		result.Message = "This entrance does not lead to that destination."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	presence, present := h.presences[client.id]
	if !present {
		result.Reason = transitionReasonNoPresence
		result.Message = "Your current position is not available yet."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if presence.state.WorldID != rule.Source.WorldID {
		result.Reason = transitionReasonSourceMismatch
		result.Message = "You are not in the entrance's source area."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if presence.state.VehicleID != nil {
		result.Reason = transitionReasonNotOnFoot
		result.Message = "Exit the vehicle before using this door."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if !rule.WithinRange(presence.state.X, presence.state.Z) {
		result.Reason = transitionReasonTooFar
		result.Message = "Move closer to the door."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if !h.travelAccess.IsRuleOpen(rule, time.UnixMilli(snapshot.GameTimeMs)) {
		result.Reason = transitionReasonClosed
		result.Message = rule.DenialMessage
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	authorizationID, err := randomHex(16)
	if err != nil {
		result.Message = "The entrance could not be authorized."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	now := time.Now()
	grant := transitionGrant{
		requestID:       request.RequestID,
		authorizationID: authorizationID,
		rule:            rule,
		expiresAt:       now.Add(rule.AuthorizationLifetime()),
	}
	h.transitionGrants[client.id] = grant
	result.Authorized = true
	result.AuthorizationID = authorizationID
	result.Reason = transitionReasonAuthorized
	result.Message = "Entrance authorized."
	result.ExpiresAtMs = grant.expiresAt.UnixMilli()
	h.mu.Unlock()
	return h.sendOne(client, result)
}

func (h *Hub) HandleTransitionCommit(
	client *Client,
	request protocol.TransitionCommit,
) bool {
	result := protocol.TransitionCommitResult{
		Header:    protocol.NewHeader(protocol.TypeTransitionCommitResult),
		RequestID: request.RequestID,
		Reason:    transitionReasonInvalidGrant,
		Message:   "This entrance authorization is no longer valid.",
	}
	now := time.Now()
	h.mu.Lock()
	if h.clients[client.id] != client {
		h.mu.Unlock()
		return false
	}
	grant, exists := h.transitionGrants[client.id]
	if !exists || grant.requestID != request.RequestID ||
		grant.authorizationID != request.AuthorizationID {
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	result.TransitionID = grant.rule.TransitionID
	result.DestinationWorldID = grant.rule.Destination.WorldID
	if !now.Before(grant.expiresAt) {
		delete(h.transitionGrants, client.id)
		result.Reason = transitionReasonExpired
		result.Message = "The door took too long to open. Try it again."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	presence, present := h.presences[client.id]
	if !present || presence.state.WorldID != grant.rule.Source.WorldID {
		delete(h.transitionGrants, client.id)
		result.Reason = transitionReasonSourceMismatch
		result.Message = "You are no longer at this entrance."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if presence.state.VehicleID != nil {
		delete(h.transitionGrants, client.id)
		result.Reason = transitionReasonNotOnFoot
		result.Message = "Exit the vehicle before using this door."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	if !grant.rule.WithinRange(presence.state.X, presence.state.Z) {
		delete(h.transitionGrants, client.id)
		result.Reason = transitionReasonTooFar
		result.Message = "You moved too far from the door. Try it again."
		h.mu.Unlock()
		return h.sendOne(client, result)
	}
	delete(h.transitionGrants, client.id)
	h.authorizedArrivals[client.id] = authorizedArrival{
		sourceWorldID:      grant.rule.Source.WorldID,
		destinationWorldID: grant.rule.Destination.WorldID,
	}
	h.mu.Unlock()

	if !h.leaveWorld(client, true) {
		h.mu.Lock()
		delete(h.authorizedArrivals, client.id)
		h.mu.Unlock()
		result.Reason = transitionReasonConnectionClosed
		result.Message = "The source area was left before travel could begin."
		return h.sendOne(client, result)
	}
	result.Committed = true
	result.Reason = transitionReasonCommitted
	result.Message = "Travel committed."
	return h.sendOne(client, result)
}

// allowPresenceDestinationLocked is the final authority boundary for world
// membership. In particular, a raw presence report cannot enter a controlled
// interior without a committed, connection-scoped arrival.
func (h *Hub) allowPresenceDestinationLocked(
	client *Client,
	message protocol.Presence,
	previous storedPresence,
	hadPrevious bool,
) bool {
	if arrival, exists := h.authorizedArrivals[client.id]; exists {
		if message.WorldID == arrival.destinationWorldID {
			delete(h.authorizedArrivals, client.id)
			delete(h.departedWorlds, client.id)
			return true
		}
		// Presence is published independently of door travel. A source update
		// already in flight when commit was processed must not consume the
		// one reserved arrival or re-add the player to the source room.
		if message.WorldID == arrival.sourceWorldID {
			return false
		}
		delete(h.authorizedArrivals, client.id)
	}

	sourceWorldID := ""
	if hadPrevious {
		sourceWorldID = previous.state.WorldID
	} else {
		sourceWorldID = h.departedWorlds[client.id]
	}
	if !h.travelAccess.ControlsDestination(message.WorldID) {
		delete(h.departedWorlds, client.id)
		if grant, exists := h.transitionGrants[client.id]; exists &&
			message.WorldID != grant.rule.Source.WorldID {
			delete(h.transitionGrants, client.id)
		}
		return true
	}
	// A player already inside may reload or rejoin the same interior. This is
	// deliberately separate from authorizing a new exterior-to-interior entry.
	if sourceWorldID == message.WorldID {
		delete(h.departedWorlds, client.id)
		return true
	}
	// Reconnect restores a server-persisted interior location. Merely claiming
	// the interior as the first presence does not work when another world was
	// persisted for the character.
	if !client.announcedInitialEntry && client.initialWorldID == message.WorldID {
		delete(h.departedWorlds, client.id)
		return true
	}
	return false
}
