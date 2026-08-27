package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/brynnb/new-yokosuka-server/internal/auth"
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

const maxScriptRequestBytes = 4 * 1024 * 1024

type ScriptStore interface {
	ListScripts(context.Context, int64) ([]store.ScriptSummary, error)
	Script(context.Context, int64, int64) (store.ScriptDetail, error)
	UpdateScriptMetadata(context.Context, int64, int64, store.ScriptMetadataUpdate) (store.ScriptDetail, error)
	SetScriptArchived(context.Context, int64, int64, bool) (store.ScriptDetail, error)
	CreateYarnScript(context.Context, int64, store.YarnScriptCreateInput) (store.ScriptDetail, error)
	SaveYarnScriptDraft(context.Context, int64, int64, int, store.YarnDraftUpdateInput) (store.ScriptVersion, error)
	CreateScriptVersion(context.Context, int64, int64, int) (store.ScriptVersion, error)
	SubmitScriptVersion(context.Context, int64, int64, int) (store.ScriptVersion, error)
	PublishScriptVersion(context.Context, int64, int64, int) (store.ScriptVersion, error)
	RollbackScriptVersion(context.Context, int64, int64, int) (store.ScriptVersion, error)
	ListScriptCollaborators(context.Context, int64, int64) ([]store.ScriptCollaborator, error)
	SetScriptCollaborator(context.Context, int64, int64, string, string) (store.ScriptCollaborator, error)
	RemoveScriptCollaborator(context.Context, int64, int64, int64) error
	ListScriptReviewThreads(context.Context, int64, int64, int) ([]store.ScriptReviewThread, error)
	CreateScriptReviewThread(context.Context, int64, int64, int, *int, string) (store.ScriptReviewThread, error)
	AddScriptReviewComment(context.Context, int64, int64, int64, string) (store.ScriptReviewComment, error)
	SetScriptReviewThreadResolved(context.Context, int64, int64, int64, bool) (store.ScriptReviewThread, error)
	ListScriptModerationEvents(context.Context, int64, int64) ([]store.ScriptModerationEvent, error)
	ListScriptTestFixtures(context.Context, int64, int64, bool) ([]store.ScriptTestFixture, error)
	CreateScriptTestFixture(context.Context, int64, int64, store.ScriptTestFixtureInput) (store.ScriptTestFixture, error)
	UpdateScriptTestFixture(context.Context, int64, int64, int64, store.ScriptTestFixtureInput) (store.ScriptTestFixture, error)
	SetScriptTestFixtureArchived(context.Context, int64, int64, int64, uint64, bool) (store.ScriptTestFixture, error)
}

type ScriptAuthenticator interface {
	FromRequest(context.Context, *http.Request) (store.Account, error)
}

type ScriptHandler struct {
	auth    ScriptAuthenticator
	store   ScriptStore
	preview ScriptPreviewer
}

type ScriptPreviewer interface {
	Preview(context.Context, scriptevent.PreviewRequest) (scriptevent.PreviewResult, error)
}

func NewScriptHandler(authenticator ScriptAuthenticator, database ScriptStore, preview ...ScriptPreviewer) *ScriptHandler {
	handler := &ScriptHandler{auth: authenticator, store: database}
	if len(preview) > 0 {
		handler.preview = preview[0]
	}
	return handler
}

func decodeScriptBody(response http.ResponseWriter, request *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxScriptRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid script request")
	}
	return nil
}

func (h *ScriptHandler) account(request *http.Request, required bool) (store.Account, error) {
	account, err := h.auth.FromRequest(request.Context(), request)
	if err != nil && !required && errors.Is(err, auth.ErrUnauthenticated) {
		return store.Account{}, nil
	}
	return account, err
}

func registered(account store.Account) bool { return account.AccountType == "registered" }

func scriptPath(path string) (scriptID int64, version int, action string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/scripts"), "/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		return id, 0, "", err == nil && id > 0
	}
	if len(parts) >= 3 && parts[1] == "versions" {
		id, idErr := strconv.ParseInt(parts[0], 10, 64)
		parsedVersion, versionErr := strconv.Atoi(parts[2])
		if idErr != nil || versionErr != nil || id <= 0 || parsedVersion <= 0 {
			return 0, 0, "", false
		}
		if len(parts) == 4 {
			action = parts[3]
		} else if len(parts) != 3 {
			return 0, 0, "", false
		}
		return id, parsedVersion, action, true
	}
	if len(parts) == 2 && parts[1] == "versions" {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		return id, 0, "versions", err == nil && id > 0
	}
	if len(parts) == 2 && parts[1] == "restore" {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		return id, 0, "restore", err == nil && id > 0
	}
	return 0, 0, "", false
}

func (h *ScriptHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	if request.URL.Path == "/api/scripts" || request.URL.Path == "/api/scripts/" {
		h.collection(response, request.WithContext(ctx))
		return
	}
	if scriptID, fixtureID, action, ok := scriptFixturePath(request.URL.Path); ok {
		h.fixtures(response, request.WithContext(ctx), scriptID, fixtureID, action)
		return
	}
	if scriptID, accountID, ok := scriptCollaboratorPath(request.URL.Path); ok {
		h.collaborators(response, request.WithContext(ctx), scriptID, accountID)
		return
	}
	if scriptID, ok := scriptModerationPath(request.URL.Path); ok {
		h.moderationEvents(response, request.WithContext(ctx), scriptID)
		return
	}
	if route, ok := scriptReviewPath(request.URL.Path); ok {
		h.reviews(response, request.WithContext(ctx), route)
		return
	}
	scriptID, version, action, ok := scriptPath(request.URL.Path)
	if !ok {
		http.NotFound(response, request)
		return
	}
	if action == "versions" {
		h.createVersion(response, request.WithContext(ctx), scriptID)
		return
	}
	if action == "restore" {
		h.setArchived(response, request.WithContext(ctx), scriptID, false)
		return
	}
	if version > 0 {
		h.version(response, request.WithContext(ctx), scriptID, version, action)
		return
	}
	h.detail(response, request.WithContext(ctx), scriptID)
}

func (h *ScriptHandler) collection(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		account, err := h.account(request, false)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "script repository unavailable")
			return
		}
		scripts, err := h.store.ListScripts(request.Context(), account.ID)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "script repository unavailable")
			return
		}
		response.Header().Set("Cache-Control", "no-cache")
		writeJSON(response, http.StatusOK, map[string]any{"scripts": scripts, "account": account})
	case http.MethodPost:
		account, err := h.account(request, true)
		if err != nil {
			writeError(response, http.StatusUnauthorized, "authentication required")
			return
		}
		if !registered(account) {
			writeError(response, http.StatusForbidden, "registered account required")
			return
		}
		var body struct {
			Slug          string                  `json:"slug"`
			Title         string                  `json:"title"`
			Description   string                  `json:"description"`
			Summary       string                  `json:"summary"`
			ContentFormat string                  `json:"contentFormat"`
			SourceText    string                  `json:"sourceText"`
			Triggers      []scriptcontent.Trigger `json:"triggers"`
		}
		if err := decodeScriptBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		if body.ContentFormat != "yarn" {
			writeError(response, http.StatusUnprocessableEntity, "new scripts must use the Yarn content format")
			return
		}
		created, err := h.store.CreateYarnScript(request.Context(), account.ID, store.YarnScriptCreateInput{
			Slug: body.Slug, Title: body.Title, Description: body.Description,
			Summary: body.Summary, SourceText: body.SourceText, Triggers: body.Triggers,
		})
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusCreated, created)
	default:
		response.Header().Set("Allow", "GET, POST")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ScriptHandler) detail(response http.ResponseWriter, request *http.Request, scriptID int64) {
	if request.Method == http.MethodDelete {
		h.setArchived(response, request, scriptID, true)
		return
	}
	if request.Method == http.MethodPatch {
		account, err := h.account(request, true)
		if err != nil || !registered(account) {
			writeError(response, http.StatusUnauthorized, "registered account required")
			return
		}
		var body struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}
		if err := decodeScriptBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		detail, err := h.store.UpdateScriptMetadata(request.Context(), account.ID, scriptID, store.ScriptMetadataUpdate{
			Title: body.Title, Description: body.Description,
		})
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "script metadata is not editable")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusOK, detail)
		return
	}
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", "GET, PATCH, DELETE")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	account, err := h.account(request, false)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "script unavailable")
		return
	}
	detail, err := h.store.Script(request.Context(), account.ID, scriptID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "script unavailable")
		return
	}
	if registered(account) && detail.AccessRole == "" {
		detail.AccessRole = "contributor"
	}
	writeJSON(response, http.StatusOK, detail)
}

func (h *ScriptHandler) setArchived(response http.ResponseWriter, request *http.Request, scriptID int64, archived bool) {
	expectedMethod := http.MethodDelete
	if !archived {
		expectedMethod = http.MethodPost
	}
	if request.Method != expectedMethod {
		response.Header().Set("Allow", expectedMethod)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	account, err := h.account(request, true)
	if err != nil || !registered(account) {
		writeError(response, http.StatusUnauthorized, "registered account required")
		return
	}
	detail, err := h.store.SetScriptArchived(
		request.Context(), account.ID, scriptID, archived,
	)
	if errors.Is(err, store.ErrForbidden) {
		writeError(response, http.StatusForbidden, "only a community script owner or moderator can change archival state")
		return
	}
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "script archival state could not be changed")
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (h *ScriptHandler) createVersion(response http.ResponseWriter, request *http.Request, scriptID int64) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", "POST")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	account, err := h.account(request, true)
	if err != nil || !registered(account) {
		writeError(response, http.StatusUnauthorized, "registered account required")
		return
	}
	var body struct {
		BasedOn int `json:"basedOn"`
	}
	if err := decodeScriptBody(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	version, err := h.store.CreateScriptVersion(request.Context(), account.ID, scriptID, body.BasedOn)
	if errors.Is(err, store.ErrForbidden) {
		writeError(response, http.StatusForbidden, "reviewers cannot create script versions")
		return
	}
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "version could not be created")
		return
	}
	writeJSON(response, http.StatusCreated, version)
}

func (h *ScriptHandler) version(response http.ResponseWriter, request *http.Request, scriptID int64, versionNumber int, action string) {
	account, err := h.account(request, true)
	if err != nil || !registered(account) {
		writeError(response, http.StatusUnauthorized, "registered account required")
		return
	}
	if action == "" && request.Method == http.MethodPut {
		var body struct {
			Revision      uint64                  `json:"revision"`
			Summary       string                  `json:"summary"`
			ContentFormat string                  `json:"contentFormat"`
			SourceText    string                  `json:"sourceText"`
			Triggers      []scriptcontent.Trigger `json:"triggers"`
		}
		if err := decodeScriptBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		if body.ContentFormat != "yarn" {
			writeError(response, http.StatusUnprocessableEntity, "only Yarn drafts are editable")
			return
		}
		version, err := h.store.SaveYarnScriptDraft(request.Context(), account.ID, scriptID, versionNumber, store.YarnDraftUpdateInput{
			Revision: body.Revision, Summary: body.Summary,
			SourceText: body.SourceText, Triggers: body.Triggers,
		})
		if errors.Is(err, store.ErrRevisionConflict) {
			writeError(response, http.StatusConflict, "script version changed; reload before saving")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusOK, version)
		return
	}
	if request.Method == http.MethodPost && action == "test" {
		h.previewVersion(response, request, account, scriptID, versionNumber)
		return
	}
	if request.Method == http.MethodPost && action == "submit" {
		version, err := h.store.SubmitScriptVersion(request.Context(), account.ID, scriptID, versionNumber)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, "script version could not be submitted")
			return
		}
		writeJSON(response, http.StatusOK, version)
		return
	}
	if request.Method == http.MethodPost && action == "publish" {
		version, err := h.store.PublishScriptVersion(request.Context(), account.ID, scriptID, versionNumber)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "moderator role required")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusOK, version)
		return
	}
	if request.Method == http.MethodPost && action == "rollback" {
		version, err := h.store.RollbackScriptVersion(
			request.Context(), account.ID, scriptID, versionNumber,
		)
		if errors.Is(err, store.ErrForbidden) {
			writeError(response, http.StatusForbidden, "only a moderator can roll back a community publication")
			return
		}
		if errors.Is(err, store.ErrRevisionConflict) {
			writeError(response, http.StatusConflict, "published version changed; reload before rolling back")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(response, http.StatusNotFound, "superseded Yarn version is unavailable")
			return
		}
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(response, http.StatusCreated, version)
		return
	}
	response.Header().Set("Allow", "PUT, POST")
	writeError(response, http.StatusMethodNotAllowed, "method not allowed")
}

func (h *ScriptHandler) previewVersion(response http.ResponseWriter, request *http.Request, account store.Account, scriptID int64, versionNumber int) {
	if h.preview == nil {
		writeError(response, http.StatusServiceUnavailable, "script preview runtime is unavailable")
		return
	}
	var body struct {
		StartNode string                     `json:"startNode"`
		Fixture   scriptevent.PreviewFixture `json:"fixture"`
	}
	if err := decodeScriptBody(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	if err := scriptevent.ValidatePreviewFixture(body.Fixture); err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	detail, err := h.store.Script(request.Context(), account.ID, scriptID)
	if err != nil {
		writeError(response, http.StatusNotFound, "script version is unavailable")
		return
	}
	var version *store.ScriptVersion
	for index := range detail.Versions {
		if detail.Versions[index].Version == versionNumber {
			version = &detail.Versions[index]
			break
		}
	}
	if version == nil || version.ContentFormat != "yarn" || version.CompileStatus != "valid" || len(version.CompiledProgram) == 0 {
		writeError(response, http.StatusUnprocessableEntity, "only a valid compiled Yarn version can be tested")
		return
	}
	foundNode := false
	for _, node := range version.CompilerNodes {
		if node.Title == body.StartNode {
			foundNode = true
			break
		}
	}
	if !foundNode {
		writeError(response, http.StatusUnprocessableEntity, "start node is not present in this compiled version")
		return
	}
	result, err := h.preview.Preview(request.Context(), scriptevent.PreviewRequest{
		Program: version.CompiledProgram, StartNode: body.StartNode,
		Lines: version.CompilerLines, Fixture: body.Fixture,
	})
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, result)
}
