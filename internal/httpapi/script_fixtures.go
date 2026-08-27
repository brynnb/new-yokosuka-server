package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func scriptFixturePath(path string) (scriptID, fixtureID int64, action string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/scripts"), "/"), "/")
	if len(parts) < 2 || parts[1] != "fixtures" {
		return 0, 0, "", false
	}
	parsedScript, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || parsedScript <= 0 {
		return 0, 0, "", false
	}
	if len(parts) == 2 {
		return parsedScript, 0, "", true
	}
	parsedFixture, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || parsedFixture <= 0 {
		return 0, 0, "", false
	}
	if len(parts) == 3 {
		return parsedScript, parsedFixture, "", true
	}
	if len(parts) == 4 && parts[3] == "restore" {
		return parsedScript, parsedFixture, "restore", true
	}
	return 0, 0, "", false
}

func (h *ScriptHandler) fixtures(response http.ResponseWriter, request *http.Request, scriptID, fixtureID int64, action string) {
	if fixtureID == 0 {
		h.fixtureCollection(response, request, scriptID)
		return
	}
	if action == "restore" {
		h.setFixtureArchived(response, request, scriptID, fixtureID, false)
		return
	}
	switch request.Method {
	case http.MethodPut:
		h.updateFixture(response, request, scriptID, fixtureID)
	case http.MethodDelete:
		h.setFixtureArchived(response, request, scriptID, fixtureID, true)
	default:
		response.Header().Set("Allow", "PUT, DELETE")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ScriptHandler) fixtureCollection(response http.ResponseWriter, request *http.Request, scriptID int64) {
	switch request.Method {
	case http.MethodGet:
		account, err := h.account(request, false)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "script fixtures unavailable")
			return
		}
		includeArchived := request.URL.Query().Get("includeArchived") == "true"
		fixtures, err := h.store.ListScriptTestFixtures(request.Context(), account.ID, scriptID, includeArchived)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "script fixtures unavailable")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"fixtures": fixtures})
	case http.MethodPost:
		account, err := h.account(request, true)
		if err != nil || !registered(account) {
			writeError(response, http.StatusUnauthorized, "registered account required")
			return
		}
		input, ok := h.decodeFixtureInput(response, request, account, scriptID, 0)
		if !ok {
			return
		}
		fixture, err := h.store.CreateScriptTestFixture(request.Context(), account.ID, scriptID, input)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "selected script version is not available")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusCreated, fixture)
	default:
		response.Header().Set("Allow", "GET, POST")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ScriptHandler) updateFixture(response http.ResponseWriter, request *http.Request, scriptID, fixtureID int64) {
	account, err := h.account(request, true)
	if err != nil || !registered(account) {
		writeError(response, http.StatusUnauthorized, "registered account required")
		return
	}
	input, ok := h.decodeFixtureInput(response, request, account, scriptID, fixtureID)
	if !ok {
		return
	}
	fixture, err := h.store.UpdateScriptTestFixture(request.Context(), account.ID, scriptID, fixtureID, input)
	if errors.Is(err, store.ErrRevisionConflict) {
		writeError(response, http.StatusConflict, "fixture changed or is not editable; reload before saving")
		return
	}
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, fixture)
}

func (h *ScriptHandler) decodeFixtureInput(response http.ResponseWriter, request *http.Request, account store.Account, scriptID, fixtureID int64) (store.ScriptTestFixtureInput, bool) {
	var body struct {
		SourceVersionID int64                      `json:"sourceVersionId"`
		Name            string                     `json:"name"`
		Description     string                     `json:"description"`
		StartNode       string                     `json:"startNode"`
		Fixture         scriptevent.PreviewFixture `json:"fixture"`
		Revision        uint64                     `json:"revision"`
	}
	if err := decodeScriptBody(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return store.ScriptTestFixtureInput{}, false
	}
	if fixtureID > 0 && body.Revision == 0 {
		writeError(response, http.StatusUnprocessableEntity, "fixture revision is required")
		return store.ScriptTestFixtureInput{}, false
	}
	if strings.TrimSpace(body.Name) == "" || len([]rune(strings.TrimSpace(body.Name))) > 120 {
		writeError(response, http.StatusUnprocessableEntity, "fixture name must be between 1 and 120 characters")
		return store.ScriptTestFixtureInput{}, false
	}
	if len([]rune(strings.TrimSpace(body.Description))) > 1000 {
		writeError(response, http.StatusUnprocessableEntity, "fixture description cannot exceed 1000 characters")
		return store.ScriptTestFixtureInput{}, false
	}
	if err := scriptevent.ValidatePreviewFixture(body.Fixture); err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return store.ScriptTestFixtureInput{}, false
	}
	detail, err := h.store.Script(request.Context(), account.ID, scriptID)
	if err != nil {
		writeError(response, http.StatusNotFound, "script version is unavailable")
		return store.ScriptTestFixtureInput{}, false
	}
	validNode := false
	for _, version := range detail.Versions {
		if version.ID != body.SourceVersionID || version.ContentFormat != "yarn" || version.CompileStatus != "valid" {
			continue
		}
		for _, node := range version.CompilerNodes {
			if node.Title == strings.TrimSpace(body.StartNode) {
				validNode = true
				break
			}
		}
		break
	}
	if !validNode {
		writeError(response, http.StatusUnprocessableEntity, "start node is not present in the selected compiled version")
		return store.ScriptTestFixtureInput{}, false
	}
	fixtureJSON, _ := json.Marshal(body.Fixture)
	return store.ScriptTestFixtureInput{
		SourceVersionID: body.SourceVersionID, Name: body.Name,
		Description: body.Description, StartNode: body.StartNode,
		Fixture: fixtureJSON, Revision: body.Revision,
	}, true
}

func (h *ScriptHandler) setFixtureArchived(response http.ResponseWriter, request *http.Request, scriptID, fixtureID int64, archived bool) {
	expected := http.MethodDelete
	if !archived {
		expected = http.MethodPost
	}
	if request.Method != expected {
		response.Header().Set("Allow", expected)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	account, err := h.account(request, true)
	if err != nil || !registered(account) {
		writeError(response, http.StatusUnauthorized, "registered account required")
		return
	}
	revision, err := strconv.ParseUint(request.URL.Query().Get("revision"), 10, 64)
	if err != nil || revision == 0 {
		writeError(response, http.StatusUnprocessableEntity, "fixture revision is required")
		return
	}
	fixture, err := h.store.SetScriptTestFixtureArchived(request.Context(), account.ID, scriptID, fixtureID, revision, archived)
	if errors.Is(err, store.ErrRevisionConflict) {
		writeError(response, http.StatusConflict, "fixture changed or is not editable; reload before changing archival state")
		return
	}
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, fixture)
}
