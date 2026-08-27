package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/worldstate"
)

func TestConfiguredOriginCanCallAdminEndpoints(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://admin.example.com")
	handler := corsMiddleware(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusOK)
	}), originsEnv())
	request := httptest.NewRequest(http.MethodOptions, "/api/admin/logs", nil)
	request.Header.Set("Origin", "https://admin.example.com")
	request.Header.Set("Access-Control-Request-Headers", "X-Admin-Token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("allowed origin = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-Admin-Token") {
		t.Fatalf("allowed headers = %q", got)
	}
}

func TestDefaultWorldEpochStartsAtEightThirtyAM(t *testing.T) {
	t.Setenv("WORLD_EPOCH_UNIX_MS", "")
	before := time.Now()
	epoch := worldEpoch()
	after := time.Now()
	if epoch.Before(before) || epoch.After(after) {
		t.Fatalf("default epoch %s was outside startup window %s–%s", epoch, before, after)
	}
	clock, err := worldstate.NewClock(epoch, "summer")
	if err != nil {
		t.Fatal(err)
	}
	gameTime := time.UnixMilli(clock.Snapshot().GameTimeMs).UTC()
	if gameTime.Hour() != 8 || gameTime.Minute() != 30 {
		t.Fatalf("default game time = %s, want 08:30", gameTime)
	}
}

func TestExplicitWorldEpochRemainsStable(t *testing.T) {
	const epochMs = int64(1_700_000_000_000)
	t.Setenv("WORLD_EPOCH_UNIX_MS", "1700000000000")
	if got := worldEpoch().UnixMilli(); got != epochMs {
		t.Fatalf("explicit epoch = %d, want %d", got, epochMs)
	}
}

func TestWorldDayLengthCannotBeOverriddenByEnvironment(t *testing.T) {
	t.Setenv("WORLD_DAY_LENGTH", "30m")
	t.Setenv("WORLD_EPOCH_UNIX_MS", "")
	clock, err := newWorldClock()
	if err != nil {
		t.Fatal(err)
	}
	if got := time.Duration(clock.Snapshot().DayLengthMs) * time.Millisecond; got != worldstate.ShenmueDayLength {
		t.Fatalf(
			"world day length = %s, want fixed %s",
			got,
			worldstate.ShenmueDayLength,
		)
	}
}
