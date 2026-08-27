package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/auth"
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type fakeScriptAuth struct {
	account store.Account
	err     error
}

func (f fakeScriptAuth) FromRequest(context.Context, *http.Request) (store.Account, error) {
	return f.account, f.err
}

type fakeScriptStore struct {
	scripts       []store.ScriptSummary
	created       store.ScriptDetail
	createAccount int64
	savedRevision uint64
	metadata      store.ScriptMetadataUpdate
	archived      *bool
	publishErr    error
	rollbackErr   error
	rollbackFrom  int
	fixtures      []store.ScriptTestFixture
	fixtureInput  store.ScriptTestFixtureInput
	fixtureState  *bool
	collaborators []store.ScriptCollaborator
	collaborator  store.ScriptCollaborator
	removed       int64
	reviewThreads []store.ScriptReviewThread
	reviewThread  store.ScriptReviewThread
	reviewComment store.ScriptReviewComment
	reviewState   *bool
	moderation    []store.ScriptModerationEvent
}

type fakeScriptPreview struct {
	request scriptevent.PreviewRequest
}

func (preview *fakeScriptPreview) Preview(_ context.Context, request scriptevent.PreviewRequest) (scriptevent.PreviewResult, error) {
	preview.request = request
	return scriptevent.PreviewResult{Outcome: "complete"}, nil
}

func (f *fakeScriptStore) ListScripts(context.Context, int64) ([]store.ScriptSummary, error) {
	return f.scripts, nil
}
func (f *fakeScriptStore) Script(context.Context, int64, int64) (store.ScriptDetail, error) {
	return f.created, nil
}
func (f *fakeScriptStore) UpdateScriptMetadata(_ context.Context, _ int64, _ int64, input store.ScriptMetadataUpdate) (store.ScriptDetail, error) {
	f.metadata = input
	f.created.Title = input.Title
	f.created.Description = input.Description
	return f.created, nil
}
func (f *fakeScriptStore) SetScriptArchived(_ context.Context, _ int64, _ int64, archived bool) (store.ScriptDetail, error) {
	f.archived = &archived
	if archived {
		now := time.Now()
		f.created.ArchivedAt = &now
	} else {
		f.created.ArchivedAt = nil
	}
	return f.created, nil
}
func (f *fakeScriptStore) CreateYarnScript(_ context.Context, accountID int64, input store.YarnScriptCreateInput) (store.ScriptDetail, error) {
	f.createAccount = accountID
	f.created = store.ScriptDetail{ScriptSummary: store.ScriptSummary{ID: 9, Slug: input.Slug, Title: input.Title}}
	return f.created, nil
}
func (f *fakeScriptStore) SaveYarnScriptDraft(_ context.Context, _ int64, _ int64, _ int, input store.YarnDraftUpdateInput) (store.ScriptVersion, error) {
	f.savedRevision = input.Revision
	return store.ScriptVersion{Version: 1, Revision: input.Revision + 1}, nil
}
func (f *fakeScriptStore) CreateScriptVersion(context.Context, int64, int64, int) (store.ScriptVersion, error) {
	return store.ScriptVersion{}, nil
}
func (f *fakeScriptStore) SubmitScriptVersion(context.Context, int64, int64, int) (store.ScriptVersion, error) {
	return store.ScriptVersion{}, nil
}
func (f *fakeScriptStore) PublishScriptVersion(context.Context, int64, int64, int) (store.ScriptVersion, error) {
	return store.ScriptVersion{}, f.publishErr
}
func (f *fakeScriptStore) RollbackScriptVersion(_ context.Context, _ int64, _ int64, version int) (store.ScriptVersion, error) {
	f.rollbackFrom = version
	return store.ScriptVersion{Version: 4, Status: "published"}, f.rollbackErr
}
func (f *fakeScriptStore) ListScriptCollaborators(context.Context, int64, int64) ([]store.ScriptCollaborator, error) {
	return f.collaborators, nil
}
func (f *fakeScriptStore) SetScriptCollaborator(_ context.Context, _ int64, _ int64, email, role string) (store.ScriptCollaborator, error) {
	f.collaborator = store.ScriptCollaborator{AccountID: 71, Email: email, Role: role}
	return f.collaborator, nil
}
func (f *fakeScriptStore) RemoveScriptCollaborator(_ context.Context, _ int64, _ int64, accountID int64) error {
	f.removed = accountID
	return nil
}
func (f *fakeScriptStore) ListScriptReviewThreads(context.Context, int64, int64, int) ([]store.ScriptReviewThread, error) {
	return f.reviewThreads, nil
}
func (f *fakeScriptStore) CreateScriptReviewThread(_ context.Context, accountID, _ int64, _ int, lineNumber *int, body string) (store.ScriptReviewThread, error) {
	f.reviewThread = store.ScriptReviewThread{ID: 81, CreatedBy: accountID, LineNumber: lineNumber, Comments: []store.ScriptReviewComment{{Body: body}}}
	return f.reviewThread, nil
}
func (f *fakeScriptStore) AddScriptReviewComment(_ context.Context, accountID, _ int64, _ int64, body string) (store.ScriptReviewComment, error) {
	f.reviewComment = store.ScriptReviewComment{ID: 82, AuthorID: accountID, Body: body}
	return f.reviewComment, nil
}
func (f *fakeScriptStore) SetScriptReviewThreadResolved(_ context.Context, _ int64, _ int64, _ int64, resolved bool) (store.ScriptReviewThread, error) {
	f.reviewState = &resolved
	status := "open"
	if resolved {
		status = "resolved"
	}
	return store.ScriptReviewThread{ID: 81, Status: status}, nil
}
func (f *fakeScriptStore) ListScriptModerationEvents(context.Context, int64, int64) ([]store.ScriptModerationEvent, error) {
	return f.moderation, nil
}
func (f *fakeScriptStore) ListScriptTestFixtures(context.Context, int64, int64, bool) ([]store.ScriptTestFixture, error) {
	return f.fixtures, nil
}
func (f *fakeScriptStore) CreateScriptTestFixture(_ context.Context, _ int64, scriptID int64, input store.ScriptTestFixtureInput) (store.ScriptTestFixture, error) {
	f.fixtureInput = input
	return store.ScriptTestFixture{ID: 31, ScriptID: scriptID, SourceVersionID: input.SourceVersionID, Name: input.Name, StartNode: input.StartNode, Fixture: input.Fixture, Revision: 1}, nil
}
func (f *fakeScriptStore) UpdateScriptTestFixture(_ context.Context, _ int64, scriptID, fixtureID int64, input store.ScriptTestFixtureInput) (store.ScriptTestFixture, error) {
	f.fixtureInput = input
	return store.ScriptTestFixture{ID: fixtureID, ScriptID: scriptID, SourceVersionID: input.SourceVersionID, Name: input.Name, StartNode: input.StartNode, Fixture: input.Fixture, Revision: input.Revision + 1}, nil
}
func (f *fakeScriptStore) SetScriptTestFixtureArchived(_ context.Context, _ int64, scriptID, fixtureID int64, revision uint64, archived bool) (store.ScriptTestFixture, error) {
	f.fixtureState = &archived
	return store.ScriptTestFixture{ID: fixtureID, ScriptID: scriptID, Revision: revision + 1}, nil
}

func TestScriptListIsPublicAndDatabaseBacked(t *testing.T) {
	database := &fakeScriptStore{scripts: []store.ScriptSummary{{ID: 7, Slug: "original-hato", Title: "Hato"}}}
	response := httptest.NewRecorder()
	NewScriptHandler(fakeScriptAuth{err: auth.ErrUnauthenticated}, database).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/scripts", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Scripts []store.ScriptSummary `json:"scripts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Scripts) != 1 || body.Scripts[0].ID != 7 {
		t.Fatalf("unexpected scripts: %#v", body.Scripts)
	}
}

func TestScriptCreationRequiresRegisteredAccount(t *testing.T) {
	database := &fakeScriptStore{}
	body := `{"slug":"new-clue","title":"New clue","description":"","summary":"","contentFormat":"yarn","sourceText":"title: Start\\n---\\nRyo: Hello\\n==="}`
	response := httptest.NewRecorder()
	NewScriptHandler(fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered", Role: "member"}}, database).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/scripts", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if database.createAccount != 42 || database.created.Slug != "new-clue" {
		t.Fatalf("creation not persisted: %#v", database)
	}
}

func TestScriptCreationRejectsGuestAccount(t *testing.T) {
	response := httptest.NewRecorder()
	NewScriptHandler(fakeScriptAuth{account: store.Account{ID: 4, AccountType: "guest"}}, &fakeScriptStore{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/scripts", strings.NewReader(`{}`)))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestRegisteredReaderCanContributeToPublicScript(t *testing.T) {
	database := &fakeScriptStore{created: store.ScriptDetail{ScriptSummary: store.ScriptSummary{ID: 8, Origin: "community"}}}
	response := httptest.NewRecorder()
	NewScriptHandler(fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered", Role: "member"}}, database).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/scripts/8", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var detail store.ScriptDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.AccessRole != "contributor" {
		t.Fatalf("access role=%q, want contributor", detail.AccessRole)
	}
}

func TestScriptMetadataUsesExplicitPatch(t *testing.T) {
	database := &fakeScriptStore{created: store.ScriptDetail{ScriptSummary: store.ScriptSummary{ID: 8}}}
	response := httptest.NewRecorder()
	NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered"}},
		database,
	).ServeHTTP(response, httptest.NewRequest(
		http.MethodPatch, "/api/scripts/8",
		strings.NewReader(`{"title":"Goro calls","description":"Telephone branch"}`),
	))
	if response.Code != http.StatusOK || database.metadata.Title != "Goro calls" || database.metadata.Description != "Telephone branch" {
		t.Fatalf("status=%d metadata=%#v body=%s", response.Code, database.metadata, response.Body.String())
	}
}

func TestCommunityScriptArchiveAndRestoreUseExplicitLifecycleRoutes(t *testing.T) {
	database := &fakeScriptStore{created: store.ScriptDetail{
		ScriptSummary: store.ScriptSummary{ID: 8, Origin: "community"},
	}}
	handler := NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered"}},
		database,
	)
	archive := httptest.NewRecorder()
	handler.ServeHTTP(archive, httptest.NewRequest(http.MethodDelete, "/api/scripts/8", nil))
	if archive.Code != http.StatusOK || database.archived == nil || !*database.archived {
		t.Fatalf("archive status=%d archived=%v body=%s", archive.Code, database.archived, archive.Body.String())
	}
	restore := httptest.NewRecorder()
	handler.ServeHTTP(restore, httptest.NewRequest(http.MethodPost, "/api/scripts/8/restore", nil))
	if restore.Code != http.StatusOK || database.archived == nil || *database.archived {
		t.Fatalf("restore status=%d archived=%v body=%s", restore.Code, database.archived, restore.Body.String())
	}
}

func TestScriptPublicationRequiresModeratorRole(t *testing.T) {
	database := &fakeScriptStore{publishErr: store.ErrForbidden}
	response := httptest.NewRecorder()
	NewScriptHandler(fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered", Role: "member"}}, database).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/scripts/8/versions/1/publish", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestModeratorRollbackCreatesANewPublishedVersion(t *testing.T) {
	database := &fakeScriptStore{}
	response := httptest.NewRecorder()
	NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered", Role: "moderator"}},
		database,
	).ServeHTTP(response, httptest.NewRequest(
		http.MethodPost, "/api/scripts/8/versions/1/rollback", nil,
	))
	if response.Code != http.StatusCreated || database.rollbackFrom != 1 {
		t.Fatalf("status=%d rollbackFrom=%d body=%s", response.Code, database.rollbackFrom, response.Body.String())
	}
	var version store.ScriptVersion
	if err := json.Unmarshal(response.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	if version.Version != 4 || version.Status != "published" {
		t.Fatalf("rollback version=%#v", version)
	}
}

func TestScriptOwnerManagesCollaboratorsThroughExplicitRoutes(t *testing.T) {
	database := &fakeScriptStore{collaborators: []store.ScriptCollaborator{{
		AccountID: 42, Email: "owner@example.test", Role: "owner",
	}}}
	handler := NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered"}},
		database,
	)
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/scripts/8/collaborators", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "owner@example.test") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	add := httptest.NewRecorder()
	handler.ServeHTTP(add, httptest.NewRequest(
		http.MethodPost, "/api/scripts/8/collaborators",
		strings.NewReader(`{"email":"editor@example.test","role":"editor"}`),
	))
	if add.Code != http.StatusOK || database.collaborator.Email != "editor@example.test" || database.collaborator.Role != "editor" {
		t.Fatalf("add status=%d collaborator=%#v body=%s", add.Code, database.collaborator, add.Body.String())
	}
	remove := httptest.NewRecorder()
	handler.ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/api/scripts/8/collaborators/71", nil))
	if remove.Code != http.StatusNoContent || database.removed != 71 {
		t.Fatalf("remove status=%d account=%d body=%s", remove.Code, database.removed, remove.Body.String())
	}
}

func TestScriptReviewThreadsAreVersionScopedAndImmutableCommentsAppend(t *testing.T) {
	database := &fakeScriptStore{reviewThreads: []store.ScriptReviewThread{{
		ID: 81, VersionID: 19, Status: "open",
		Comments: []store.ScriptReviewComment{{ID: 80, Body: "Keep the original branch."}},
	}}}
	handler := NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered"}},
		database,
	)
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(
		http.MethodGet, "/api/scripts/8/versions/2/review-threads", nil,
	))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "Keep the original branch.") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	create := httptest.NewRecorder()
	handler.ServeHTTP(create, httptest.NewRequest(
		http.MethodPost, "/api/scripts/8/versions/2/review-threads",
		strings.NewReader(`{"lineNumber":12,"body":"This condition needs a fixture."}`),
	))
	if create.Code != http.StatusCreated || database.reviewThread.LineNumber == nil || *database.reviewThread.LineNumber != 12 {
		t.Fatalf("create status=%d thread=%#v body=%s", create.Code, database.reviewThread, create.Body.String())
	}
	reply := httptest.NewRecorder()
	handler.ServeHTTP(reply, httptest.NewRequest(
		http.MethodPost, "/api/scripts/8/review-threads/81/comments",
		strings.NewReader(`{"body":"Fixture added."}`),
	))
	if reply.Code != http.StatusCreated || database.reviewComment.Body != "Fixture added." {
		t.Fatalf("reply status=%d comment=%#v body=%s", reply.Code, database.reviewComment, reply.Body.String())
	}
	resolve := httptest.NewRecorder()
	handler.ServeHTTP(resolve, httptest.NewRequest(
		http.MethodPost, "/api/scripts/8/review-threads/81/resolve", nil,
	))
	if resolve.Code != http.StatusOK || database.reviewState == nil || !*database.reviewState {
		t.Fatalf("resolve status=%d state=%v body=%s", resolve.Code, database.reviewState, resolve.Body.String())
	}
}

func TestCollaboratorCanReadConsolidatedModerationHistory(t *testing.T) {
	database := &fakeScriptStore{moderation: []store.ScriptModerationEvent{{
		ID: 91, ScriptID: 8, ActorID: 42, Action: "version.published",
	}}}
	response := httptest.NewRecorder()
	NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered"}},
		database,
	).ServeHTTP(response, httptest.NewRequest(
		http.MethodGet, "/api/scripts/8/moderation-events", nil,
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "version.published") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestValidYarnVersionCanBePreviewedWithoutCharacterMutation(t *testing.T) {
	database := &fakeScriptStore{created: store.ScriptDetail{
		ScriptSummary: store.ScriptSummary{ID: 8, Origin: "community"},
		Versions: []store.ScriptVersion{{
			Version: 1, ContentFormat: "yarn", CompileStatus: "valid",
			CompiledProgram: []byte{1, 2, 3},
			CompilerNodes:   []scriptcontent.CompiledNode{{Title: "Start"}},
		}},
	}}
	preview := &fakeScriptPreview{}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/scripts/8/versions/1/test",
		strings.NewReader(`{"startNode":"Start","fixture":{"scene":"D000","flags":{"story.ready":true}}}`),
	)
	NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered"}},
		database,
		preview,
	).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if preview.request.StartNode != "Start" || !preview.request.Fixture.Flags["story.ready"] {
		t.Fatalf("preview request=%#v", preview.request)
	}
}

func TestScriptFixtureCRUDUsesVisibleCompiledVersion(t *testing.T) {
	database := &fakeScriptStore{created: store.ScriptDetail{
		ScriptSummary: store.ScriptSummary{ID: 8, Origin: "community"},
		Versions: []store.ScriptVersion{{
			ID: 19, Version: 2, ContentFormat: "yarn", CompileStatus: "valid",
			CompilerNodes: []scriptcontent.CompiledNode{{Title: "HatoTalk"}},
		}},
	}}
	handler := NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered"}},
		database,
	)
	create := httptest.NewRecorder()
	handler.ServeHTTP(create, httptest.NewRequest(
		http.MethodPost, "/api/scripts/8/fixtures",
		strings.NewReader(`{"sourceVersionId":19,"name":"After finding Charlie","description":"Hato branch","startNode":"HatoTalk","fixture":{"scene":"D000","flags":{"story.charlie-found":true},"gameHour":14}}`),
	))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	if database.fixtureInput.SourceVersionID != 19 || database.fixtureInput.StartNode != "HatoTalk" {
		t.Fatalf("fixture input=%#v", database.fixtureInput)
	}
	var saved store.ScriptTestFixture
	if err := json.Unmarshal(create.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	update := httptest.NewRecorder()
	handler.ServeHTTP(update, httptest.NewRequest(
		http.MethodPut, "/api/scripts/8/fixtures/31",
		strings.NewReader(`{"sourceVersionId":19,"name":"After Charlie","description":"Updated","startNode":"HatoTalk","revision":1,"fixture":{"scene":"D000","yen":100}}`),
	))
	if update.Code != http.StatusOK || database.fixtureInput.Revision != 1 {
		t.Fatalf("update status=%d input=%#v body=%s", update.Code, database.fixtureInput, update.Body.String())
	}
	archive := httptest.NewRecorder()
	handler.ServeHTTP(archive, httptest.NewRequest(http.MethodDelete, "/api/scripts/8/fixtures/31?revision=2", nil))
	if archive.Code != http.StatusOK || database.fixtureState == nil || !*database.fixtureState {
		t.Fatalf("archive status=%d state=%v body=%s", archive.Code, database.fixtureState, archive.Body.String())
	}
}

func TestScriptFixtureRejectsNodeOutsidePinnedVersion(t *testing.T) {
	database := &fakeScriptStore{created: store.ScriptDetail{
		Versions: []store.ScriptVersion{{
			ID: 19, ContentFormat: "yarn", CompileStatus: "valid",
			CompilerNodes: []scriptcontent.CompiledNode{{Title: "HatoTalk"}},
		}},
	}}
	response := httptest.NewRecorder()
	NewScriptHandler(
		fakeScriptAuth{account: store.Account{ID: 42, AccountType: "registered"}}, database,
	).ServeHTTP(response, httptest.NewRequest(
		http.MethodPost, "/api/scripts/8/fixtures",
		strings.NewReader(`{"sourceVersionId":19,"name":"Wrong node","startNode":"Telephone","fixture":{}}`),
	))
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "start node") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
