package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func scriptCollaboratorPath(path string) (scriptID, accountID int64, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/scripts"), "/"), "/")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, 0, false
	}
	if parts[1] != "collaborators" {
		return 0, 0, false
	}
	parsedScriptID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || parsedScriptID <= 0 {
		return 0, 0, false
	}
	if len(parts) == 2 {
		return parsedScriptID, 0, true
	}
	parsedAccountID, err := strconv.ParseInt(parts[2], 10, 64)
	return parsedScriptID, parsedAccountID, err == nil && parsedAccountID > 0
}

func (h *ScriptHandler) collaborators(
	response http.ResponseWriter,
	request *http.Request,
	scriptID, collaboratorAccountID int64,
) {
	account, err := h.account(request, true)
	if err != nil || !registered(account) {
		writeError(response, http.StatusUnauthorized, "registered account required")
		return
	}
	if collaboratorAccountID > 0 {
		if request.Method != http.MethodDelete {
			response.Header().Set("Allow", "DELETE")
			writeError(response, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		err := h.store.RemoveScriptCollaborator(
			request.Context(), account.ID, scriptID, collaboratorAccountID,
		)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "only the community script owner can remove collaborators")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(response, http.StatusNotFound, "collaborator is unavailable")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, "collaborator could not be removed")
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}

	switch request.Method {
	case http.MethodGet:
		collaborators, err := h.store.ListScriptCollaborators(
			request.Context(), account.ID, scriptID,
		)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "script collaborators are private")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, "collaborators could not be loaded")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"collaborators": collaborators})
	case http.MethodPost:
		var body struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if err := decodeScriptBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		collaborator, err := h.store.SetScriptCollaborator(
			request.Context(), account.ID, scriptID, body.Email, body.Role,
		)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "only the community script owner can manage collaborators")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(response, http.StatusNotFound, "registered account was not found")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusOK, collaborator)
	default:
		response.Header().Set("Allow", "GET, POST")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
	}
}
