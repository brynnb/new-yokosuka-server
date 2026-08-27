package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/brynnb/new-yokosuka-server/internal/game"
)

type Progression struct {
	Experience int64 `json:"experience"`
	Level      int   `json:"level"`
	CurrentHP  int   `json:"currentHp"`
	MaxHP      int   `json:"maxHp"`
}

func progressionFor(experience int64, currentHP, maxHP int) Progression {
	level := game.LevelForExperience(experience)
	return Progression{
		Experience: experience,
		Level:      level,
		CurrentHP:  min(max(0, currentHP), maxHP),
		MaxHP:      maxHP,
	}
}

func (s *Store) Progression(ctx context.Context, characterID int64) (Progression, error) {
	var experience int64
	var currentHP, maxHP int
	err := s.db.QueryRowContext(ctx, `
		SELECT experience, current_hp, max_hp
		FROM characters
		WHERE id = $1 AND deleted_at IS NULL`,
		characterID,
	).Scan(&experience, &currentHP, &maxHP)
	if errors.Is(err, sql.ErrNoRows) {
		return Progression{}, ErrNotFound
	}
	if err != nil {
		return Progression{}, err
	}
	return progressionFor(experience, currentHP, maxHP), nil
}

func (s *Store) AwardExperience(
	ctx context.Context,
	characterID,
	amount int64,
	eventKey,
	reason string,
) (Progression, error) {
	if amount <= 0 {
		return Progression{}, errors.New("experience amount must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Progression{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO progression_events
			(character_id, event_key, kind, experience_delta, reason)
		VALUES ($1, NULLIF($2, ''), 'experience', $3, $4)`,
		characterID,
		eventKey,
		amount,
		reason,
	)
	if isUniqueViolation(err) {
		return Progression{}, ErrDuplicateEvent
	}
	if err != nil {
		return Progression{}, err
	}
	var oldExperience int64
	var currentHP, maxHP int
	if err := tx.QueryRowContext(ctx, `
		SELECT experience, current_hp, max_hp FROM characters
		WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		characterID,
	).Scan(&oldExperience, &currentHP, &maxHP); err != nil {
		return Progression{}, err
	}
	newExperience := oldExperience + amount
	if _, err := tx.ExecContext(ctx, `
		UPDATE characters
		SET experience = $2, updated_at = now()
		WHERE id = $1`,
		characterID,
		newExperience,
	); err != nil {
		return Progression{}, err
	}
	if err := tx.Commit(); err != nil {
		return Progression{}, err
	}
	return progressionFor(newExperience, currentHP, maxHP), nil
}

// ApplyDamage reduces a character's persisted HP without coupling health to
// experience or level. eventKey makes gameplay retries idempotent when set.
func (s *Store) ApplyDamage(
	ctx context.Context,
	characterID int64,
	amount int,
	eventKey,
	reason string,
) (Progression, error) {
	if amount <= 0 {
		return Progression{}, errors.New("damage amount must be positive")
	}
	return s.changeHP(ctx, characterID, -amount, "damage", eventKey, reason)
}

// Heal restores a character's persisted HP up to their independently stored
// maximum. eventKey makes gameplay retries idempotent when set.
func (s *Store) Heal(
	ctx context.Context,
	characterID int64,
	amount int,
	eventKey,
	reason string,
) (Progression, error) {
	if amount <= 0 {
		return Progression{}, errors.New("healing amount must be positive")
	}
	return s.changeHP(ctx, characterID, amount, "heal", eventKey, reason)
}

func (s *Store) changeHP(
	ctx context.Context,
	characterID int64,
	delta int,
	kind,
	eventKey,
	reason string,
) (Progression, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Progression{}, err
	}
	defer tx.Rollback()
	progression, err := changeHPInTx(
		ctx, tx, characterID, delta, kind, eventKey, reason,
	)
	if err != nil {
		return Progression{}, err
	}
	if err := tx.Commit(); err != nil {
		return Progression{}, err
	}
	return progression, nil
}

func changeHPInTx(
	ctx context.Context,
	tx *sql.Tx,
	characterID int64,
	delta int,
	kind,
	eventKey,
	reason string,
) (Progression, error) {
	var experience int64
	var currentHP, maxHP int
	err := tx.QueryRowContext(ctx, `
		SELECT experience, current_hp, max_hp
		FROM characters
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE`, characterID,
	).Scan(&experience, &currentHP, &maxHP)
	if errors.Is(err, sql.ErrNoRows) {
		return Progression{}, ErrNotFound
	}
	if err != nil {
		return Progression{}, err
	}
	newHP := min(maxHP, max(0, currentHP+delta))
	if _, err := tx.ExecContext(ctx, `
		UPDATE characters
		SET current_hp = $2, updated_at = now()
		WHERE id = $1`, characterID, newHP,
	); err != nil {
		return Progression{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO progression_events
			(character_id, event_key, kind, hp_delta, reason)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5)`,
		characterID, eventKey, kind, newHP-currentHP, reason,
	)
	if isUniqueViolation(err) {
		return Progression{}, ErrDuplicateEvent
	}
	if err != nil {
		return Progression{}, err
	}
	return progressionFor(experience, newHP, maxHP), nil
}

func (s *Store) UseHealingItem(
	ctx context.Context,
	characterID int64,
	itemKey string,
) (Progression, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Progression{}, 0, err
	}
	defer tx.Rollback()
	var healAmount int
	var quantity int
	err = tx.QueryRowContext(ctx, `
		SELECT item.effect_value, inventory.quantity
		FROM character_inventory inventory
		JOIN item_definitions item ON item.key = inventory.item_key
		WHERE inventory.character_id = $1
		  AND inventory.item_key = $2
		  AND item.usable = true
		  AND item.effect_kind = 'heal_hp'
		FOR UPDATE OF inventory`,
		characterID,
		itemKey,
	).Scan(&healAmount, &quantity)
	if errors.Is(err, sql.ErrNoRows) {
		return Progression{}, 0, ErrInsufficient
	}
	if err != nil {
		return Progression{}, 0, err
	}
	var experience int64
	var currentHP, maxHP int
	if err := tx.QueryRowContext(ctx, `
		SELECT experience, current_hp, max_hp FROM characters
		WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		characterID,
	).Scan(&experience, &currentHP, &maxHP); err != nil {
		return Progression{}, 0, err
	}
	if currentHP >= maxHP {
		return Progression{}, quantity, errors.New("hp is already full")
	}
	remaining := quantity - 1
	if remaining == 0 {
		_, err = tx.ExecContext(ctx, `
			DELETE FROM character_inventory
			WHERE character_id = $1 AND item_key = $2`,
			characterID,
			itemKey,
		)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE character_inventory SET quantity = $3, updated_at = now()
			WHERE character_id = $1 AND item_key = $2`,
			characterID,
			itemKey,
			remaining,
		)
	}
	if err != nil {
		return Progression{}, 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO economy_events
			(character_id, kind, item_key, quantity_delta, reason)
		VALUES ($1, 'item_consume', $2, -1, 'heal_hp')`,
		characterID,
		itemKey,
	); err != nil {
		return Progression{}, 0, err
	}
	progression, err := changeHPInTx(
		ctx,
		tx,
		characterID,
		healAmount,
		"heal",
		"",
		fmt.Sprintf("item:%s", itemKey),
	)
	if err != nil {
		return Progression{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return Progression{}, 0, err
	}
	return progression, remaining, nil
}
