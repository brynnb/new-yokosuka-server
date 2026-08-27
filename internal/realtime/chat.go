package realtime

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/brynnb/new-yokosuka-server/internal/activitylog"
	"github.com/brynnb/new-yokosuka-server/internal/contentfilter"
	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type ChatMessageSaver interface {
	SaveChatMessage(context.Context, store.ChatMessageLog) error
}

type ChatHistoryLoader interface {
	RecentChatMessages(
		context.Context,
		int,
	) ([]store.ChatMessageLog, error)
}

func (h *Hub) SetChatMessageSaver(saver ChatMessageSaver) {
	h.mu.Lock()
	h.chatMessageSaver = saver
	h.chatHistoryLoader, _ = saver.(ChatHistoryLoader)
	h.mu.Unlock()
}

func (h *Hub) SetPublicChatSink(sink func(senderName, text string)) {
	h.mu.Lock()
	h.publicChatSink = sink
	h.mu.Unlock()
}

func (h *Hub) emitPublicChat(senderName, text string) {
	h.mu.RLock()
	sink := h.publicChatSink
	h.mu.RUnlock()
	if sink != nil {
		sink(senderName, text)
	}
}

func (h *Hub) HandleChat(client *Client, rawText string) bool {
	text, ok := normalizeChat(rawText)
	if !ok {
		h.rejectChat(client, "Message must contain 1–240 readable characters.")
		return false
	}
	text = contentfilter.CensorChat(text)
	now := time.Now()
	h.mu.RLock()
	presence, exists := h.presences[client.id]
	if !exists {
		h.mu.RUnlock()
		h.rejectChat(client, "Join a world before chatting.")
		return false
	}
	name := client.name
	recipients := h.allClientsLocked("")
	connectedClients := len(h.clients)
	saver := h.chatMessageSaver
	h.mu.RUnlock()
	if allowed, reason := client.allowChat(now); !allowed {
		h.rejectChat(client, reason)
		return false
	}
	if saver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := saver.SaveChatMessage(ctx, store.ChatMessageLog{
			AccountID:   client.accountID,
			CharacterID: client.characterID,
			PlayerID:    client.id,
			PlayerName:  name,
			WorldID:     presence.state.WorldID,
			Text:        text,
			RemoteIP:    client.remoteIP,
			UserAgent:   client.userAgent,
			SentAt:      now,
		})
		cancel()
		if err != nil {
			h.logf("chat message save failed for %s: %v", client.id, err)
		}
	}
	h.recordActivity(activitylog.Event{
		Timestamp:        now,
		Type:             "chat",
		PlayerID:         client.id,
		Name:             name,
		WorldID:          presence.state.WorldID,
		Text:             text,
		RemoteIP:         client.remoteIP,
		UserAgent:        client.userAgent,
		ConnectedClients: connectedClients,
	})

	h.sendMany(recipients, protocol.ChatEvent{
		Header: protocol.NewHeader(protocol.TypeChat),
		Message: protocol.ChatMessage{
			PlayerID: client.id,
			Name:     name,
			WorldID:  presence.state.WorldID,
			Text:     text,
			SentAt:   now.UnixMilli(),
		},
	})
	h.emitPublicChat(name, text)
	return true
}

// BroadcastExternalChat inserts a verified service message into the same
// global chat stream, persistence, history, and content filter as player chat.
func (h *Hub) BroadcastExternalChat(rawName, rawText string) error {
	text, ok := normalizeChat(rawText)
	if !ok {
		return errors.New("message must contain 1–240 readable characters")
	}
	name := strings.Join(strings.Fields(rawName), " ")
	if name == "" {
		return errors.New("sender name is required")
	}
	nameRunes := []rune(name)
	if len(nameRunes) > 80 {
		name = string(nameRunes[:80])
	}
	text = contentfilter.CensorChat(text)
	now := time.Now()
	h.mu.RLock()
	recipients := h.allClientsLocked("")
	connectedClients := len(h.clients)
	saver := h.chatMessageSaver
	h.mu.RUnlock()
	if saver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := saver.SaveChatMessage(ctx, store.ChatMessageLog{
			PlayerID: "discord:" + name, PlayerName: name, WorldID: "discord",
			Text: text, RemoteIP: "discord-bridge", UserAgent: "discord-bridge", SentAt: now,
		})
		cancel()
		if err != nil {
			return err
		}
	}
	h.recordActivity(activitylog.Event{
		Timestamp: now, Type: "chat", PlayerID: "discord:" + name, Name: name,
		WorldID: "discord", Text: text, RemoteIP: "discord-bridge",
		UserAgent: "discord-bridge", ConnectedClients: connectedClients,
	})
	h.sendMany(recipients, protocol.ChatEvent{
		Header: protocol.NewHeader(protocol.TypeChat),
		Message: protocol.ChatMessage{
			PlayerID: "discord:" + name, Name: name, WorldID: "discord", Text: text, SentAt: now.UnixMilli(),
		},
	})
	h.emitPublicChat(name, text)
	return nil
}

func normalizeChat(text string) (string, bool) {
	if !utf8.ValidString(text) {
		return "", false
	}
	for _, value := range text {
		if unicode.IsControl(value) && !unicode.IsSpace(value) {
			return "", false
		}
	}
	normalized := strings.Join(strings.Fields(text), " ")
	count := utf8.RuneCountInString(normalized)
	return normalized, count >= 1 && count <= 240
}

func (h *Hub) rejectChat(client *Client, reason string) {
	h.sendOne(client, protocol.ChatRejected{
		Header: protocol.NewHeader(protocol.TypeChatRejected),
		Reason: reason,
	})
}
