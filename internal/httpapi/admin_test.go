package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/realtime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type fakeAdminSessions struct {
	sessions []realtime.AdminSession
}

func (f fakeAdminSessions) AdminSessions() []realtime.AdminSession {
	return f.sessions
}

type fakeAdminChats struct {
	messages []store.ChatMessageLog
	limit    int
	growth   store.AdminGrowthEvents
}

type fakeAdminLogs struct {
	lines []string
}

func (f fakeAdminLogs) Lines() []string {
	return append([]string(nil), f.lines...)
}

func (f *fakeAdminChats) RecentAdminChatMessages(
	_ context.Context,
	limit int,
) ([]store.ChatMessageLog, error) {
	f.limit = limit
	return f.messages, nil
}

func (f *fakeAdminChats) AdminGrowthEvents(context.Context) (store.AdminGrowthEvents, error) {
	return f.growth, nil
}

func TestAdminHandlerRequiresAdminToken(t *testing.T) {
	handler := NewAdminHandler("secret", fakeAdminSessions{}, &fakeAdminChats{}, time.Now())
	response := httptest.NewRecorder()
	handler.Stats(response, httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestAdminHandlerReturnsSessionsAndOneHundredChats(t *testing.T) {
	now := time.Now()
	chatSource := &fakeAdminChats{messages: []store.ChatMessageLog{{
		PlayerName: "Ryo",
		WorldID:    "dobuita",
		Text:       "Hello",
		SentAt:     now,
	}}}
	handler := NewAdminHandler("secret", fakeAdminSessions{sessions: []realtime.AdminSession{{
		ID: "one", Name: "Ryo", WorldID: "dobuita",
	}}}, chatSource, now.Add(-time.Minute))

	statsRequest := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	statsRequest.Header.Set("X-Admin-Token", "secret")
	statsResponse := httptest.NewRecorder()
	handler.Stats(statsResponse, statsRequest)
	var stats struct {
		OnlineUsers int `json:"online_users"`
	}
	if err := json.Unmarshal(statsResponse.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if statsResponse.Code != http.StatusOK || stats.OnlineUsers != 1 {
		t.Fatalf("unexpected stats response: %d %s", statsResponse.Code, statsResponse.Body.String())
	}

	chatRequest := httptest.NewRequest(http.MethodGet, "/api/admin/chats?limit=100", nil)
	chatRequest.Header.Set("X-Admin-Token", "secret")
	chatResponse := httptest.NewRecorder()
	handler.Chats(chatResponse, chatRequest)
	var chats []map[string]any
	if err := json.Unmarshal(chatResponse.Body.Bytes(), &chats); err != nil {
		t.Fatal(err)
	}
	if chatResponse.Code != http.StatusOK || chatSource.limit != 100 || len(chats) != 1 {
		t.Fatalf("unexpected chat response: limit=%d status=%d body=%s", chatSource.limit, chatResponse.Code, chatResponse.Body.String())
	}
	if chats[0]["sender"] != "Ryo" || chats[0]["location"] != "dobuita" {
		t.Fatalf("unexpected normalized chat: %#v", chats[0])
	}
}

func TestAdminHandlerReturnsBufferedLogs(t *testing.T) {
	handler := NewAdminHandler(
		"secret",
		fakeAdminSessions{},
		&fakeAdminChats{},
		time.Now(),
		fakeAdminLogs{lines: []string{"first", "second"}},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/logs", nil)
	request.Header.Set("X-Admin-Token", "secret")
	response := httptest.NewRecorder()
	handler.Logs(response, request)

	var body struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(body.Lines) != 2 || body.Lines[1] != "second" {
		t.Fatalf("unexpected logs response: %d %s", response.Code, response.Body.String())
	}
}

func TestAdminHandlerReturnsDailyCumulativeGrowth(t *testing.T) {
	now := time.Now().UTC()
	source := &fakeAdminChats{growth: store.AdminGrowthEvents{
		Users:      []time.Time{now.Add(-48 * time.Hour), now.Add(-time.Hour)},
		Characters: []time.Time{now.Add(-time.Hour)},
		LastLogins: []time.Time{now.Add(-time.Hour), now.Add(-48 * time.Hour), now.Add(-10 * 24 * time.Hour)},
	}}
	handler := NewAdminHandler("secret", fakeAdminSessions{}, source, now)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/growth?days=30", nil)
	request.Header.Set("X-Admin-Token", "secret")
	response := httptest.NewRecorder()
	handler.Growth(response, request)

	var body struct {
		TotalUsers int                 `json:"total_users"`
		Points     []adminGrowthPoint  `json:"points"`
		Activity   adminActivityCounts `json:"activity"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || body.TotalUsers != 2 || len(body.Points) != 30 || body.Points[29].TotalCharacters != 1 || body.Activity.Daily != 1 || body.Activity.Weekly != 2 || body.Activity.Monthly != 3 {
		t.Fatalf("unexpected growth response: %d %s", response.Code, response.Body.String())
	}
}
