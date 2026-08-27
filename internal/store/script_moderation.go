package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type ScriptModerationEvent struct {
	ID        int64          `json:"id"`
	ScriptID  int64          `json:"scriptId"`
	VersionID *int64         `json:"versionId,omitempty"`
	ActorID   int64          `json:"actorId"`
	Action    string         `json:"action"`
	Details   map[string]any `json:"details"`
	CreatedAt time.Time      `json:"createdAt"`
}

type moderationEventExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertScriptModerationEvent(
	ctx context.Context,
	executor moderationEventExecutor,
	scriptID int64,
	versionID *int64,
	actorID int64,
	action string,
	details map[string]any,
) error {
	if details == nil {
		details = map[string]any{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode script moderation event: %w", err)
	}
	if _, err := executor.ExecContext(ctx, `
		INSERT INTO script_moderation_events (
			script_id,version_id,actor_id,action,details
		) SELECT $1,$2,$3,$4,$5
		WHERE EXISTS (SELECT 1 FROM scripts WHERE id=$1 AND origin='community')
	`, scriptID, versionID, actorID, action, encoded); err != nil {
		return fmt.Errorf("append script moderation event: %w", err)
	}
	return nil
}

func (s *Store) ListScriptModerationEvents(
	ctx context.Context,
	viewerAccountID, scriptID int64,
) ([]ScriptModerationEvent, error) {
	var allowed bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM scripts script
			JOIN accounts viewer ON viewer.id=$1 AND viewer.account_type='registered'
			LEFT JOIN script_collaborators collaborator
			  ON collaborator.script_id=script.id AND collaborator.account_id=viewer.id
			WHERE script.id=$2 AND script.origin='community'
			  AND (collaborator.account_id IS NOT NULL OR viewer.role IN ('moderator','admin'))
		)
	`, viewerAccountID, scriptID).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,script_id,version_id,actor_id,action,details,created_at
		FROM script_moderation_events
		WHERE script_id=$1
		ORDER BY created_at DESC,id DESC
	`, scriptID)
	if err != nil {
		return nil, fmt.Errorf("list script moderation events: %w", err)
	}
	defer rows.Close()
	events := []ScriptModerationEvent{}
	for rows.Next() {
		var event ScriptModerationEvent
		var versionID sql.NullInt64
		var details []byte
		if err := rows.Scan(
			&event.ID, &event.ScriptID, &versionID, &event.ActorID,
			&event.Action, &details, &event.CreatedAt,
		); err != nil {
			return nil, err
		}
		if versionID.Valid {
			value := versionID.Int64
			event.VersionID = &value
		}
		if err := json.Unmarshal(details, &event.Details); err != nil {
			return nil, fmt.Errorf("decode script moderation event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
