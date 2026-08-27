package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npc"
)

func (s *Store) LoadNPCCheckpoints(ctx context.Context) ([]npc.Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT npc_id, day_number, accumulated_delay_seconds, revision, updated_at
		FROM npc_runtime_state
		ORDER BY npc_id`)
	if err != nil {
		return nil, fmt.Errorf("load NPC checkpoints: %w", err)
	}
	defer rows.Close()
	checkpoints := make([]npc.Checkpoint, 0)
	for rows.Next() {
		var checkpoint npc.Checkpoint
		if err := rows.Scan(
			&checkpoint.NPCID,
			&checkpoint.DayNumber,
			&checkpoint.AccumulatedDelay,
			&checkpoint.Revision,
			&checkpoint.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan NPC checkpoint: %w", err)
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate NPC checkpoints: %w", err)
	}
	return checkpoints, nil
}

func (s *Store) SaveNPCCheckpoints(
	ctx context.Context,
	checkpoints []npc.Checkpoint,
) error {
	if len(checkpoints) == 0 {
		return nil
	}
	type checkpointJSON struct {
		NPCID            string  `json:"npc_id"`
		DayNumber        int64   `json:"day_number"`
		AccumulatedDelay float64 `json:"accumulated_delay"`
		Revision         uint64  `json:"revision"`
		UpdatedAt        string  `json:"updated_at"`
	}
	payload := make([]checkpointJSON, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		updatedAt := checkpoint.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = time.Now()
		}
		payload = append(payload, checkpointJSON{
			NPCID:            checkpoint.NPCID,
			DayNumber:        checkpoint.DayNumber,
			AccumulatedDelay: checkpoint.AccumulatedDelay,
			Revision:         checkpoint.Revision,
			UpdatedAt:        updatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode NPC checkpoints: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO npc_runtime_state (
			npc_id,
			day_number,
			accumulated_delay_seconds,
			revision,
			updated_at
		)
		SELECT
			row.npc_id,
			row.day_number,
			row.accumulated_delay,
			row.revision,
			row.updated_at
		FROM jsonb_to_recordset($1::jsonb) AS row(
			npc_id text,
			day_number bigint,
			accumulated_delay double precision,
			revision bigint,
			updated_at timestamptz
		)
		ON CONFLICT (npc_id) DO UPDATE SET
			day_number = EXCLUDED.day_number,
			accumulated_delay_seconds = EXCLUDED.accumulated_delay_seconds,
			revision = EXCLUDED.revision,
			updated_at = EXCLUDED.updated_at`,
		encoded,
	); err != nil {
		return fmt.Errorf("save NPC checkpoints: %w", err)
	}
	return nil
}
