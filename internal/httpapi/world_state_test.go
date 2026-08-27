package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

type fakeWorldState struct {
	state protocol.WorldState
}

func (f fakeWorldState) Snapshot() protocol.WorldState {
	return f.state
}

func (f fakeWorldState) SetGameSecond(second int) (protocol.WorldState, error) {
	f.state.GameTimeMs = int64(second)
	return f.state, nil
}

func TestWorldStateHandler(t *testing.T) {
	expected := protocol.WorldState{
		DayLengthMs: 1_200_000,
		Season:      "winter", SeasonIndex: 1,
		Weather: "snow", WeatherIndex: 3,
	}
	response := httptest.NewRecorder()
	NewWorldStateHandler(fakeWorldState{state: expected}).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/world-state", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var actual protocol.WorldState
	if err := json.Unmarshal(response.Body.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	if actual.DayLengthMs != expected.DayLengthMs || actual.Season != "winter" ||
		actual.Weather != "snow" {
		t.Fatalf("unexpected response: %#v", actual)
	}
}

func TestWorldStateHandlerSetsGameTimeAndNotifies(t *testing.T) {
	notified := protocol.WorldState{}
	response := httptest.NewRecorder()
	NewWorldStateHandler(
		fakeWorldState{},
		func(state protocol.WorldState) { notified = state },
	).ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPatch,
			"/api/world-state",
			strings.NewReader(`{"gameSecond":45600}`),
		),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if notified.GameTimeMs != 45600 {
		t.Fatalf("callback received unexpected state: %#v", notified)
	}
}
