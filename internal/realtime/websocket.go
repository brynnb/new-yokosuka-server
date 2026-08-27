package realtime

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/auth"
	"github.com/brynnb/new-yokosuka-server/internal/store"
	"github.com/gorilla/websocket"
)

func requestRemoteIP(request *http.Request) string {
	forwarded := strings.TrimSpace(request.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

type WebSocketHandler struct {
	hub            *Hub
	allowedOrigins map[string]struct{}
	upgrader       websocket.Upgrader
	auth           *auth.Manager
	store          *store.Store
}

func NewAuthenticatedWebSocketHandler(
	hub *Hub,
	allowedOrigins []string,
	manager *auth.Manager,
	database *store.Store,
) *WebSocketHandler {
	handler := NewWebSocketHandler(hub, allowedOrigins)
	handler.auth = manager
	handler.store = database
	return handler
}

func NewWebSocketHandler(hub *Hub, allowedOrigins []string) *WebSocketHandler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		normalized := strings.TrimRight(strings.TrimSpace(origin), "/")
		if normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	handler := &WebSocketHandler{hub: hub, allowedOrigins: allowed}
	handler.upgrader = websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
		CheckOrigin:      handler.checkOrigin,
	}
	return handler
}

func (h *WebSocketHandler) checkOrigin(request *http.Request) bool {
	origin := strings.TrimRight(request.Header.Get("Origin"), "/")
	if origin == "" {
		return true
	}
	if _, ok := h.allowedOrigins[origin]; ok {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, request.Host)
}

func (h *WebSocketHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	metadata := ConnectionMetadata{
		RemoteIP:  requestRemoteIP(request),
		UserAgent: request.UserAgent(),
	}
	if h.auth != nil && h.store != nil {
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		account, err := h.auth.FromRequest(ctx, request)
		if err != nil {
			http.Error(response, "authentication required", http.StatusUnauthorized)
			return
		}
		characterID, err := strconv.ParseInt(request.URL.Query().Get("characterId"), 10, 64)
		if err != nil || characterID <= 0 {
			http.Error(response, "valid characterId required", http.StatusBadRequest)
			return
		}
		state, err := h.store.CharacterState(ctx, account.ID, characterID)
		if err != nil {
			http.Error(response, "character not found", http.StatusForbidden)
			return
		}
		metadata.AccountID = account.ID
		metadata.AccountType = account.AccountType
		metadata.Character = &state.Character
		metadata.Inventory = state.Inventory
		dialogueState, err := h.store.DialogueState(
			ctx,
			account.ID,
			characterID,
		)
		if err != nil {
			http.Error(response, "dialogue state unavailable", http.StatusInternalServerError)
			return
		}
		metadata.DialogueState = &dialogueState
	}
	conn, err := h.upgrader.Upgrade(response, request, nil)
	if err != nil {
		return
	}
	client, err := h.hub.Register(conn, metadata)
	if err != nil {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, err.Error()),
			time.Now().Add(writeWait),
		)
		_ = conn.Close()
		return
	}
	if metadata.Character != nil && h.store != nil {
		loginCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := h.store.MarkCharacterLogin(
			loginCtx,
			metadata.Character.ID,
		); err != nil {
			h.hub.logf(
				"mark character %d login failed: %v",
				metadata.Character.ID,
				err,
			)
		}
		cancel()
	}
	go client.writePump()
	client.readPump()
}
