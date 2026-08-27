package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/realtime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type adminSessionSource interface {
	AdminSessions() []realtime.AdminSession
}

type adminChatSource interface {
	RecentAdminChatMessages(context.Context, int) ([]store.ChatMessageLog, error)
}

type adminLogSource interface {
	Lines() []string
}

type adminGrowthSource interface {
	AdminGrowthEvents(context.Context) (store.AdminGrowthEvents, error)
}

type AdminHandler struct {
	adminKey string
	sessions adminSessionSource
	chats    adminChatSource
	logs     adminLogSource
	growth   adminGrowthSource
	started  time.Time
}

func NewAdminHandler(
	adminKey string,
	sessions adminSessionSource,
	chats adminChatSource,
	started time.Time,
	logs ...adminLogSource,
) *AdminHandler {
	handler := &AdminHandler{
		adminKey: strings.TrimSpace(adminKey),
		sessions: sessions,
		chats:    chats,
		started:  started,
	}
	if len(logs) > 0 {
		handler.logs = logs[0]
	}
	if growth, ok := chats.(adminGrowthSource); ok {
		handler.growth = growth
	}
	return handler
}

type adminGrowthPoint struct {
	Date            string `json:"date"`
	TotalUsers      int    `json:"total_users"`
	TotalCharacters int    `json:"total_characters"`
}

type adminActivityCounts struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

func buildAdminActivity(lastLogins []time.Time, now time.Time) adminActivityCounts {
	counts := adminActivityCounts{}
	for _, login := range lastLogins {
		if !login.Before(now.Add(-24 * time.Hour)) {
			counts.Daily++
		}
		if !login.Before(now.Add(-7 * 24 * time.Hour)) {
			counts.Weekly++
		}
		if !login.Before(now.Add(-30 * 24 * time.Hour)) {
			counts.Monthly++
		}
	}
	return counts
}

func buildAdminGrowthPoints(events store.AdminGrowthEvents, days int, now time.Time) []adminGrowthPoint {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -(days - 1))
	points := make([]adminGrowthPoint, 0, days)
	for offset := 0; offset < days; offset++ {
		day := start.AddDate(0, 0, offset)
		cutoff := day.AddDate(0, 0, 1)
		point := adminGrowthPoint{Date: day.Format("2006-01-02")}
		for _, createdAt := range events.Users {
			if createdAt.Before(cutoff) {
				point.TotalUsers++
			}
		}
		for _, createdAt := range events.Characters {
			if createdAt.Before(cutoff) {
				point.TotalCharacters++
			}
		}
		points = append(points, point)
	}
	return points
}

func (h *AdminHandler) Growth(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorize(response, request) {
		return
	}
	days := 365
	if raw := request.URL.Query().Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != 30 && parsed != 90 && parsed != 365) {
			writeError(response, http.StatusBadRequest, "days must be 30, 90, or 365")
			return
		}
		days = parsed
	}
	if h.growth == nil {
		writeError(response, http.StatusServiceUnavailable, "growth data unavailable")
		return
	}
	events, err := h.growth.AdminGrowthEvents(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "could not load growth history")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"total_users":      len(events.Users),
		"total_characters": len(events.Characters),
		"points":           buildAdminGrowthPoints(events, days, time.Now()),
		"activity":         buildAdminActivity(events.LastLogins, time.Now()),
	})
}

func (h *AdminHandler) Logs(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorize(response, request) {
		return
	}

	lines := []string{}
	if h.logs != nil {
		lines = h.logs.Lines()
	}
	writeJSON(response, http.StatusOK, map[string]any{"lines": lines})
}

func (h *AdminHandler) authorize(response http.ResponseWriter, request *http.Request) bool {
	provided := request.Header.Get("X-Admin-Token")
	if h.adminKey == "" || len(provided) != len(h.adminKey) ||
		subtle.ConstantTimeCompare([]byte(provided), []byte(h.adminKey)) != 1 {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (h *AdminHandler) Stats(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorize(response, request) {
		return
	}

	sessions := h.sessions.AdminSessions()
	writeJSON(response, http.StatusOK, map[string]any{
		"online_users":  len(sessions),
		"uptime":        time.Since(h.started).Round(time.Second).String(),
		"live_sessions": sessions,
	})
}

func (h *AdminHandler) Chats(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorize(response, request) {
		return
	}

	limit := store.MaxAdminRecentChatMessages
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > store.MaxAdminRecentChatMessages {
			writeError(response, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}

	messages, err := h.chats.RecentAdminChatMessages(request.Context(), limit)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "could not load chat history")
		return
	}
	type adminChatMessage struct {
		Sender    string `json:"sender"`
		Text      string `json:"text"`
		Channel   string `json:"channel"`
		Location  string `json:"location,omitempty"`
		Timestamp int64  `json:"timestamp"`
	}
	result := make([]adminChatMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, adminChatMessage{
			Sender:    message.PlayerName,
			Text:      message.Text,
			Channel:   "global",
			Location:  message.WorldID,
			Timestamp: message.SentAt.UnixMilli(),
		})
	}
	writeJSON(response, http.StatusOK, result)
}
