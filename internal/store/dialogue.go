package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/brynnb/new-yokosuka-server/internal/dialoguestate"
)

func (s *Store) DialogueState(
	ctx context.Context,
	accountID,
	characterID int64,
) (dialoguestate.Snapshot, error) {
	var revision sql.NullInt64
	var encoded []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT dialogue.revision, dialogue.snapshot
		FROM characters character
		LEFT JOIN character_dialogue_state dialogue
			ON dialogue.character_id = character.id
		WHERE character.id = $1
		  AND character.account_id = $2
		  AND character.deleted_at IS NULL`,
		characterID,
		accountID,
	).Scan(&revision, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return dialoguestate.Snapshot{}, ErrNotFound
	}
	if err != nil {
		return dialoguestate.Snapshot{}, fmt.Errorf("load dialogue state: %w", err)
	}
	if !revision.Valid {
		return dialoguestate.Default(), nil
	}
	var snapshot dialoguestate.Snapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return dialoguestate.Snapshot{}, fmt.Errorf("decode dialogue state: %w", err)
	}
	if revision.Int64 < 0 || snapshot.Revision != uint64(revision.Int64) {
		return dialoguestate.Snapshot{}, errors.New("dialogue state revision is inconsistent")
	}
	if err := snapshot.Validate(); err != nil {
		return dialoguestate.Snapshot{}, fmt.Errorf("validate dialogue state: %w", err)
	}
	return snapshot, nil
}

func (s *Store) SaveDialogueState(
	ctx context.Context,
	accountID,
	characterID int64,
	snapshot dialoguestate.Snapshot,
) (dialoguestate.Snapshot, error) {
	if snapshot.Revision >= math.MaxInt64 {
		return dialoguestate.Snapshot{}, errors.New("dialogue state revision is exhausted")
	}
	if err := snapshot.Validate(); err != nil {
		return dialoguestate.Snapshot{}, fmt.Errorf("validate dialogue state: %w", err)
	}
	expectedRevision := snapshot.Revision
	snapshot.Revision++
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return dialoguestate.Snapshot{}, fmt.Errorf("encode dialogue state: %w", err)
	}
	var savedRevision int64
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO character_dialogue_state
			(character_id, revision, snapshot)
		SELECT character.id, $3, $4::jsonb
		FROM characters character
		WHERE character.id = $1
		  AND character.account_id = $2
		  AND character.deleted_at IS NULL
		  AND (
			$5 = 0
			OR EXISTS (
				SELECT 1
				FROM character_dialogue_state current
				WHERE current.character_id = character.id
				  AND current.revision = $5
			)
		  )
		ON CONFLICT (character_id) DO UPDATE
		SET revision = $3, snapshot = $4::jsonb, updated_at = now()
		WHERE character_dialogue_state.revision = $5
		RETURNING revision`,
		characterID,
		accountID,
		int64(snapshot.Revision),
		encoded,
		int64(expectedRevision),
	).Scan(&savedRevision)
	if errors.Is(err, sql.ErrNoRows) {
		if _, ownershipErr := s.CharacterForAccount(
			ctx,
			accountID,
			characterID,
		); errors.Is(ownershipErr, ErrNotFound) {
			return dialoguestate.Snapshot{}, ErrNotFound
		}
		return dialoguestate.Snapshot{}, ErrRevisionConflict
	}
	if err != nil {
		return dialoguestate.Snapshot{}, fmt.Errorf("save dialogue state: %w", err)
	}
	if uint64(savedRevision) != snapshot.Revision {
		return dialoguestate.Snapshot{}, errors.New("saved dialogue revision is inconsistent")
	}
	return snapshot, nil
}
