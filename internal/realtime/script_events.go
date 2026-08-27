package realtime

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

const scriptEventRequestTimeout = 10 * time.Second

var scriptEventRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{7,79}$`)

type ScriptEventEngine interface {
	Start(context.Context, int64, int64, scriptcontent.TriggerSelector, scriptevent.WorldFacts) (scriptevent.Yield, error)
	Advance(context.Context, int64, int64, int64, scriptruntime.Input) (scriptevent.Yield, error)
}

func (h *Hub) SetScriptEngine(engine ScriptEventEngine, areaWorlds map[string]string) {
	inverse := make(map[string]string, len(areaWorlds))
	for area, worldID := range areaWorlds {
		if existing, found := inverse[worldID]; found && existing != area {
			// An ambiguous browser world cannot safely select a native-area trigger.
			inverse[worldID] = ""
			continue
		}
		inverse[worldID] = area
	}
	h.mu.Lock()
	h.scriptEvents = engine
	h.nativeAreaForWorld = inverse
	h.mu.Unlock()
}

func (h *Hub) sendScriptYield(client *Client, requestID string, yield scriptevent.Yield) bool {
	return h.sendOne(client, protocol.ScriptEventYield{
		Header: protocol.NewHeader(protocol.TypeScriptEventYield), RequestID: requestID,
		RunID: yield.RunID, ScriptID: yield.ScriptID, VersionID: yield.VersionID,
		ScriptSlug: yield.ScriptSlug, Event: yield.RuntimeEvent,
		Line: yield.Line, Options: yield.Options, State: yield.State,
	})
}

func (h *Hub) rejectScriptEvent(client *Client, requestID string, runID int64, code, message string) bool {
	return h.sendOne(client, protocol.ScriptEventRejected{
		Header:    protocol.NewHeader(protocol.TypeScriptEventRejected),
		RequestID: requestID, RunID: runID, Code: code, Message: message,
	})
}

func (h *Hub) HandleScriptEventStart(client *Client, request protocol.ScriptEventStartRequest) bool {
	if !scriptEventRequestIDPattern.MatchString(request.RequestID) {
		return h.rejectScriptEvent(client, request.RequestID, 0, "invalid_request", "Invalid script event request.")
	}
	h.mu.RLock()
	current := h.clients[client.id]
	engine := h.scriptEvents
	area := h.nativeAreaForWorld[client.worldID()]
	npcs := h.npcs
	presence, hasPresence := h.presences[client.id]
	h.mu.RUnlock()
	if current != client || !client.persistent || engine == nil {
		return h.rejectScriptEvent(client, request.RequestID, 0, "unavailable", "Scripted interactions are unavailable.")
	}
	if client.scriptRun() != 0 {
		return h.rejectScriptEvent(client, request.RequestID, client.scriptRun(), "event_active", "Finish the current interaction first.")
	}
	selector := scriptcontent.TriggerSelector{
		Kind: request.Kind, Actor: request.Actor, Object: request.Object, Activity: request.Activity,
	}
	if request.Kind != "activity" {
		if area == "" {
			return h.rejectScriptEvent(client, request.RequestID, 0, "area_unavailable", "This location has no unambiguous native script area.")
		}
		selector.Area = area
	}
	selector, err := scriptcontent.NormalizeTriggerSelector(selector)
	if err != nil {
		return h.rejectScriptEvent(client, request.RequestID, 0, "invalid_request", "Invalid script trigger selector.")
	}
	if selector.Kind == "automatic" && !client.claimAutomaticScriptDispatch(client.worldID()) {
		return h.rejectScriptEvent(client, request.RequestID, 0, "automatic_already_dispatched", "Automatic story events have already been evaluated for this room entry.")
	}
	world := h.world.Snapshot()
	gameTime := time.UnixMilli(world.GameTimeMs).UTC()
	gameHour := float64(gameTime.Hour()) + float64(gameTime.Minute())/60 + float64(gameTime.Second())/3600
	gameDate := scriptevent.CalendarDateFromTime(gameTime)
	facts := scriptevent.WorldFacts{GameHour: &gameHour, GameDate: &gameDate}
	if hasPresence && presence.state.WorldID == client.worldID() {
		facts.ActorBounds = scriptevent.ReviewedActorBounds(
			area, "AKIR", presence.state.X, presence.state.Y,
			presence.state.Z, presence.state.Yaw,
		)
	}
	if npcs != nil {
		facts.ActorPresence = npcs.ActorPresence(client.worldID())
	}
	ctx, cancel := context.WithTimeout(context.Background(), scriptEventRequestTimeout)
	defer cancel()
	yield, err := engine.Start(ctx, client.accountID, client.characterID, selector, facts)
	if err != nil {
		return h.handleScriptError(client, request.RequestID, 0, err)
	}
	if !terminalScriptYield(yield.RuntimeEvent.Type) {
		client.setScriptRun(yield.RunID)
	}
	return h.sendScriptYield(client, request.RequestID, yield)
}

func (h *Hub) HandleScriptEventAdvance(client *Client, request protocol.ScriptEventAdvanceRequest) bool {
	if !scriptEventRequestIDPattern.MatchString(request.RequestID) || request.RunID <= 0 || request.RunID != client.scriptRun() {
		return h.rejectScriptEvent(client, request.RequestID, request.RunID, "invalid_request", "Invalid script event response.")
	}
	var input scriptruntime.Input
	switch request.Action {
	case "continue":
		if request.OptionID != nil {
			return h.rejectScriptEvent(client, request.RequestID, request.RunID, "invalid_request", "Continue does not accept an option.")
		}
		input = scriptruntime.Continue()
	case "select":
		if request.OptionID == nil {
			return h.rejectScriptEvent(client, request.RequestID, request.RunID, "invalid_request", "An option is required.")
		}
		input = scriptruntime.Select(*request.OptionID)
	case "cancel":
		if request.OptionID != nil {
			return h.rejectScriptEvent(client, request.RequestID, request.RunID, "invalid_request", "Cancel does not accept an option.")
		}
		input = scriptruntime.Cancel()
	default:
		return h.rejectScriptEvent(client, request.RequestID, request.RunID, "invalid_request", "Unknown script event response.")
	}
	h.mu.RLock()
	engine := h.scriptEvents
	h.mu.RUnlock()
	if engine == nil {
		return h.rejectScriptEvent(client, request.RequestID, request.RunID, "unavailable", "Scripted interactions are unavailable.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), scriptEventRequestTimeout)
	defer cancel()
	yield, err := engine.Advance(ctx, client.accountID, client.characterID, request.RunID, input)
	if err != nil {
		client.setScriptRun(0)
		return h.handleScriptError(client, request.RequestID, request.RunID, err)
	}
	if terminalScriptYield(yield.RuntimeEvent.Type) {
		client.setScriptRun(0)
	}
	return h.sendScriptYield(client, request.RequestID, yield)
}

func terminalScriptYield(eventType string) bool {
	return eventType == "complete" || eventType == "cancelled" || eventType == "declined"
}

func (h *Hub) handleScriptError(client *Client, requestID string, runID int64, err error) bool {
	code, message := "runtime_error", "The scripted interaction could not continue."
	switch {
	case errors.Is(err, store.ErrNotFound):
		code, message = "no_script", "No published script matches this interaction."
	case errors.Is(err, store.ErrScriptEventActive):
		code, message = "event_active", "Finish the current interaction first."
	case errors.Is(err, store.ErrAmbiguousTrigger):
		code, message = "ambiguous_trigger", "This interaction has conflicting published scripts."
	case errors.Is(err, store.ErrScriptEventEnded):
		code, message = "event_ended", "This interaction has expired or already ended."
	case errors.Is(err, store.ErrInsufficient):
		code, message = "insufficient_state", "The required money or item is no longer available."
	}
	h.logf("script event failed for character %d run %d: %v", client.characterID, runID, err)
	return h.rejectScriptEvent(client, requestID, runID, code, message)
}

func (h *Hub) cancelClientScript(client *Client) {
	runID := client.scriptRun()
	if runID == 0 {
		return
	}
	h.mu.RLock()
	engine := h.scriptEvents
	h.mu.RUnlock()
	client.setScriptRun(0)
	if engine == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), scriptEventRequestTimeout)
	defer cancel()
	if _, err := engine.Advance(ctx, client.accountID, client.characterID, runID, scriptruntime.Cancel()); err != nil && !errors.Is(err, store.ErrNotFound) {
		h.logf("cancel disconnected character script event %d: %v", runID, err)
	}
}
