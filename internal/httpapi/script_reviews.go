package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type scriptReviewRoute struct {
	scriptID int64
	version  int
	threadID int64
	action   string
}

func scriptReviewPath(path string) (scriptReviewRoute, bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/scripts"), "/"), "/")
	if len(parts) != 4 {
		return scriptReviewRoute{}, false
	}
	scriptID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || scriptID <= 0 {
		return scriptReviewRoute{}, false
	}
	if parts[1] == "versions" && parts[3] == "review-threads" {
		version, err := strconv.Atoi(parts[2])
		return scriptReviewRoute{scriptID: scriptID, version: version, action: "threads"}, err == nil && version > 0
	}
	if parts[1] != "review-threads" {
		return scriptReviewRoute{}, false
	}
	threadID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || threadID <= 0 || (parts[3] != "comments" && parts[3] != "resolve" && parts[3] != "reopen") {
		return scriptReviewRoute{}, false
	}
	return scriptReviewRoute{scriptID: scriptID, threadID: threadID, action: parts[3]}, true
}

func (h *ScriptHandler) reviews(response http.ResponseWriter, request *http.Request, route scriptReviewRoute) {
	if route.action == "threads" && request.Method == http.MethodGet {
		account, err := h.account(request, false)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "script review unavailable")
			return
		}
		threads, err := h.store.ListScriptReviewThreads(
			request.Context(), account.ID, route.scriptID, route.version,
		)
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(response, request)
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, "script review unavailable")
			return
		}
		response.Header().Set("Cache-Control", "no-cache")
		writeJSON(response, http.StatusOK, map[string]any{"threads": threads})
		return
	}
	account, err := h.account(request, true)
	if err != nil || !registered(account) {
		writeError(response, http.StatusUnauthorized, "registered account required")
		return
	}
	switch route.action {
	case "threads":
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", "GET, POST")
			writeError(response, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			LineNumber *int   `json:"lineNumber"`
			Body       string `json:"body"`
		}
		if err := decodeScriptBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		thread, err := h.store.CreateScriptReviewThread(
			request.Context(), account.ID, route.scriptID, route.version, body.LineNumber, body.Body,
		)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "this script version is not open for review")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusCreated, thread)
	case "comments":
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", "POST")
			writeError(response, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			Body string `json:"body"`
		}
		if err := decodeScriptBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		comment, err := h.store.AddScriptReviewComment(
			request.Context(), account.ID, route.scriptID, route.threadID, body.Body,
		)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "this review thread is not writable")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusCreated, comment)
	case "resolve", "reopen":
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", "POST")
			writeError(response, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		thread, err := h.store.SetScriptReviewThreadResolved(
			request.Context(), account.ID, route.scriptID, route.threadID, route.action == "resolve",
		)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "this review thread cannot be changed")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusOK, thread)
	}
}
