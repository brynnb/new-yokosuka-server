package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type fakeScriptSchemaStore struct {
	identifiers []store.ScriptIdentifierCatalogEntry
}

func (f fakeScriptSchemaStore) ListPublishedScriptIdentifiers(context.Context) ([]store.ScriptIdentifierCatalogEntry, error) {
	return f.identifiers, nil
}

func TestScriptSchemaIsPublicAndPinned(t *testing.T) {
	response := httptest.NewRecorder()
	NewScriptSchemaHandler(fakeScriptSchemaStore{identifiers: []store.ScriptIdentifierCatalogEntry{
		{Kind: "flag", Identifier: "hato.met", UsageCount: 1},
	}}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/script-schema", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, fragment := range []string{
		scriptcontent.YarnCommandSchemaVersion, "flag_set", "start_activity",
		"pass_trigger", "hato.met", "d000.hato.camera.2950",
		"Recovered D000 Hato player position",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("schema body is missing %q: %s", fragment, body)
		}
	}
}
