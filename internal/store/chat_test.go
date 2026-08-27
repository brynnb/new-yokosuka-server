package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSaveChatMessageValidatesBeforeDatabaseAccess(t *testing.T) {
	database := &Store{}
	valid := ChatMessageLog{
		PlayerID:   "connection-1",
		PlayerName: "Ryo",
		WorldID:    "dobuita",
		Text:       "Hello",
		SentAt:     time.Now(),
	}
	tests := []ChatMessageLog{
		{},
		func() ChatMessageLog {
			message := valid
			message.Text = ""
			return message
		}(),
		func() ChatMessageLog {
			message := valid
			message.Text = strings.Repeat("a", 241)
			return message
		}(),
		func() ChatMessageLog {
			message := valid
			message.SentAt = time.Time{}
			return message
		}(),
	}
	for _, message := range tests {
		if err := database.SaveChatMessage(
			context.Background(),
			message,
		); err == nil {
			t.Fatalf("invalid chat message was accepted: %#v", message)
		}
	}
}

func TestRecentChatMessagesValidatesLimitBeforeDatabaseAccess(t *testing.T) {
	database := &Store{}
	for _, limit := range []int{0, MaxRecentChatMessages + 1} {
		if _, err := database.RecentChatMessages(
			context.Background(),
			limit,
		); err == nil {
			t.Fatalf("invalid recent chat limit %d was accepted", limit)
		}
	}
}
