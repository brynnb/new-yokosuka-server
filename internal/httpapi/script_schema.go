package httpapi

import (
	"context"
	"net/http"
	"sort"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type ScriptSchemaStore interface {
	ListPublishedScriptIdentifiers(context.Context) ([]store.ScriptIdentifierCatalogEntry, error)
}

type ScriptSchemaHandler struct {
	store ScriptSchemaStore
}

type scriptSchemaIdentifier struct {
	Kind        string `json:"kind"`
	Identifier  string `json:"identifier"`
	Description string `json:"description,omitempty"`
	UsageCount  int    `json:"usageCount"`
}

func NewScriptSchemaHandler(database ScriptSchemaStore) *ScriptSchemaHandler {
	return &ScriptSchemaHandler{store: database}
}

func (h *ScriptSchemaHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	registry, err := scriptcontent.Registry()
	if err != nil {
		writeError(response, http.StatusInternalServerError, "script command schema unavailable")
		return
	}
	identifiers, err := h.store.ListPublishedScriptIdentifiers(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "script identifier catalog unavailable")
		return
	}
	merged := make(map[string]scriptSchemaIdentifier, len(registry.Capabilities)+len(identifiers))
	for _, capability := range registry.Capabilities {
		key := capability.Kind + "\x00" + capability.Identifier
		merged[key] = scriptSchemaIdentifier{
			Kind: capability.Kind, Identifier: capability.Identifier,
			Description: capability.Description,
		}
	}
	for _, identifier := range identifiers {
		key := identifier.Kind + "\x00" + identifier.Identifier
		entry := merged[key]
		entry.Kind, entry.Identifier = identifier.Kind, identifier.Identifier
		entry.UsageCount = identifier.UsageCount
		merged[key] = entry
	}
	result := make([]scriptSchemaIdentifier, 0, len(merged))
	for _, identifier := range merged {
		result = append(result, identifier)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		return result[left].Identifier < result[right].Identifier
	})
	response.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(response, http.StatusOK, struct {
		scriptcontent.CommandRegistry
		Identifiers []scriptSchemaIdentifier `json:"identifiers"`
	}{CommandRegistry: registry, Identifiers: result})
}
