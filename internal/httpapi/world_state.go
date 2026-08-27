package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

type WorldStateProvider interface {
	Snapshot() protocol.WorldState
	SetGameSecond(int) (protocol.WorldState, error)
}

type WorldStateHandler struct {
	provider  WorldStateProvider
	onChanged func(protocol.WorldState)
}

func NewWorldStateHandler(
	provider WorldStateProvider,
	onChanged ...func(protocol.WorldState),
) *WorldStateHandler {
	handler := &WorldStateHandler{provider: provider}
	if len(onChanged) > 0 {
		handler.onChanged = onChanged[0]
	}
	return handler
}

func (h *WorldStateHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		writeJSON(response, http.StatusOK, h.provider.Snapshot())
	case http.MethodPatch:
		h.setGameTime(response, request)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPatch)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WorldStateHandler) setGameTime(
	response http.ResponseWriter,
	request *http.Request,
) {
	var input struct {
		GameSecond *int `json:"gameSecond"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.GameSecond == nil {
		writeError(response, http.StatusBadRequest, "gameSecond is required")
		return
	}
	state, err := h.provider.SetGameSecond(*input.GameSecond)
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	if h.onChanged != nil {
		h.onChanged(state)
	}
	writeJSON(response, http.StatusOK, state)
}
