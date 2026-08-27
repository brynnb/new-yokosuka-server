package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusHandlerReportsServerOnline(t *testing.T) {
	response := httptest.NewRecorder()
	NewStatusHandler().ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/status", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"online":true`) {
		t.Fatalf("body = %q", response.Body.String())
	}
}
