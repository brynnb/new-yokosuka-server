package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func scriptModerationPath(path string) (int64, bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/scripts"), "/"), "/")
	if len(parts) != 2 || parts[1] != "moderation-events" {
		return 0, false
	}
	scriptID, err := strconv.ParseInt(parts[0], 10, 64)
	return scriptID, err == nil && scriptID > 0
}

func (h *ScriptHandler) moderationEvents(response http.ResponseWriter, request *http.Request, scriptID int64) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", "GET")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	account, err := h.account(request, true)
	if err != nil || !registered(account) {
		writeError(response, http.StatusUnauthorized, "registered account required")
		return
	}
	events, err := h.store.ListScriptModerationEvents(request.Context(), account.ID, scriptID)
	if errors.Is(err, store.ErrForbidden) {
		writeError(response, http.StatusForbidden, "moderation history is private to collaborators")
		return
	}
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "moderation history unavailable")
		return
	}
	response.Header().Set("Cache-Control", "no-cache")
	writeJSON(response, http.StatusOK, map[string]any{"events": events})
}
