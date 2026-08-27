package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type ChatMessageLog struct {
	AccountID   int64
	CharacterID int64
	PlayerID    string
	PlayerName  string
	WorldID     string
	Text        string
	RemoteIP    string
	UserAgent   string
	SentAt      time.Time
}

const MaxRecentChatMessages = 20
const MaxAdminRecentChatMessages = 100

func (s *Store) SaveChatMessage(
	ctx context.Context,
	message ChatMessageLog,
) error {
	message.PlayerID = strings.TrimSpace(message.PlayerID)
	message.PlayerName = strings.TrimSpace(message.PlayerName)
	message.WorldID = strings.TrimSpace(message.WorldID)
	if message.PlayerID == "" ||
		message.PlayerName == "" ||
		message.WorldID == "" ||
		message.Text == "" {
		return errors.New("chat message identity, world, and text are required")
	}
	if count := utf8.RuneCountInString(message.Text); count > 240 {
		return errors.New("chat message exceeds 240 characters")
	}
	if message.SentAt.IsZero() {
		return errors.New("chat message timestamp is required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chat_messages (
			account_id,
			character_id,
			player_id,
			player_name,
			world_id,
			message_text,
			remote_ip,
			user_agent,
			sent_at
		) VALUES (
			NULLIF($1, 0),
			NULLIF($2, 0),
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9
		)`,
		message.AccountID,
		message.CharacterID,
		message.PlayerID,
		message.PlayerName,
		message.WorldID,
		message.Text,
		message.RemoteIP,
		message.UserAgent,
		message.SentAt,
	)
	if err != nil {
		return fmt.Errorf("save chat message: %w", err)
	}
	return nil
}

// RecentChatMessages returns the newest persisted public chat messages in
// display order (oldest first).
func (s *Store) RecentChatMessages(
	ctx context.Context,
	limit int,
) ([]ChatMessageLog, error) {
	if limit < 1 || limit > MaxRecentChatMessages {
		return nil, fmt.Errorf(
			"recent chat message limit must be between 1 and %d",
			MaxRecentChatMessages,
		)
	}
	return s.recentChatMessages(ctx, limit)
}

// RecentAdminChatMessages permits the larger history window used by the
// authenticated operations dashboard without changing the in-game replay
// window.
func (s *Store) RecentAdminChatMessages(
	ctx context.Context,
	limit int,
) ([]ChatMessageLog, error) {
	if limit < 1 || limit > MaxAdminRecentChatMessages {
		return nil, fmt.Errorf(
			"admin recent chat message limit must be between 1 and %d",
			MaxAdminRecentChatMessages,
		)
	}
	return s.recentChatMessages(ctx, limit)
}

func (s *Store) recentChatMessages(
	ctx context.Context,
	limit int,
) ([]ChatMessageLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(account_id, 0),
			COALESCE(character_id, 0),
			player_id,
			player_name,
			world_id,
			message_text,
			remote_ip,
			user_agent,
			sent_at
		FROM chat_messages
		ORDER BY sent_at DESC, id DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("load recent chat messages: %w", err)
	}
	defer rows.Close()

	messages := make([]ChatMessageLog, 0, limit)
	for rows.Next() {
		var message ChatMessageLog
		if err := rows.Scan(
			&message.AccountID,
			&message.CharacterID,
			&message.PlayerID,
			&message.PlayerName,
			&message.WorldID,
			&message.Text,
			&message.RemoteIP,
			&message.UserAgent,
			&message.SentAt,
		); err != nil {
			return nil, fmt.Errorf("scan recent chat message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent chat messages: %w", err)
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}
