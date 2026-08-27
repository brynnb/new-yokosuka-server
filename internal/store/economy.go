package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type inventoryQuerier interface {
	QueryContext(
		context.Context,
		string,
		...any,
	) (*sql.Rows, error)
}

func inventoryWithQuerier(
	ctx context.Context,
	querier inventoryQuerier,
	characterID int64,
) ([]InventoryItem, error) {
	rows, err := querier.QueryContext(ctx, `
		SELECT item.key, item.name, item.description, item.category,
		       item.max_stack, item.usable,
		       COALESCE(item.effect_kind, ''), COALESCE(item.effect_value, 0),
		       inventory.quantity
		FROM character_inventory inventory
		JOIN item_definitions item ON item.key = inventory.item_key
		WHERE inventory.character_id = $1
		ORDER BY item.category, item.name`,
		characterID,
	)
	if err != nil {
		return nil, fmt.Errorf("load inventory: %w", err)
	}
	defer rows.Close()
	items := make([]InventoryItem, 0)
	for rows.Next() {
		var item InventoryItem
		if err := rows.Scan(
			&item.Key,
			&item.Name,
			&item.Description,
			&item.Category,
			&item.MaxStack,
			&item.Usable,
			&item.EffectKind,
			&item.EffectValue,
			&item.Quantity,
		); err != nil {
			return nil, fmt.Errorf("scan inventory: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Inventory(
	ctx context.Context,
	characterID int64,
) ([]InventoryItem, error) {
	return inventoryWithQuerier(ctx, s.db, characterID)
}

func (s *Store) CharacterState(
	ctx context.Context,
	accountID,
	characterID int64,
) (CharacterState, error) {
	character, err := s.CharacterForAccount(ctx, accountID, characterID)
	if err != nil {
		return CharacterState{}, err
	}
	items, err := s.Inventory(ctx, characterID)
	if err != nil {
		return CharacterState{}, err
	}
	return CharacterState{Character: character, Inventory: items}, nil
}

func recordEconomyEvent(
	ctx context.Context,
	tx *sql.Tx,
	characterID int64,
	eventKey,
	kind,
	itemKey string,
	quantityDelta int,
	yenDelta int64,
	reason string,
) error {
	var nullableItem any
	if itemKey != "" {
		nullableItem = itemKey
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO economy_events
			(character_id, event_key, kind, item_key, quantity_delta, yen_delta, reason)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7)`,
		characterID,
		eventKey,
		kind,
		nullableItem,
		quantityDelta,
		yenDelta,
		reason,
	)
	if isUniqueViolation(err) {
		return ErrDuplicateEvent
	}
	return err
}

func (s *Store) CreditYen(
	ctx context.Context,
	characterID,
	amount int64,
	eventKey,
	reason string,
) (int64, error) {
	if amount <= 0 {
		return 0, errors.New("credit amount must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := recordEconomyEvent(ctx, tx, characterID, eventKey, "yen_credit", "", 0, amount, reason); err != nil {
		return 0, err
	}
	var balance int64
	if err := tx.QueryRowContext(ctx, `
		UPDATE characters SET yen = yen + $2, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING yen`,
		characterID,
		amount,
	).Scan(&balance); err != nil {
		return 0, err
	}
	return balance, tx.Commit()
}

func (s *Store) SpendYen(
	ctx context.Context,
	characterID,
	amount int64,
	eventKey,
	reason string,
) (int64, error) {
	if amount <= 0 {
		return 0, errors.New("spend amount must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var balance int64
	err = tx.QueryRowContext(ctx, `
		UPDATE characters SET yen = yen - $2, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND yen >= $2
		RETURNING yen`,
		characterID,
		amount,
	).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrInsufficient
	}
	if err != nil {
		return 0, err
	}
	if err := recordEconomyEvent(ctx, tx, characterID, eventKey, "yen_spend", "", 0, -amount, reason); err != nil {
		return 0, err
	}
	return balance, tx.Commit()
}

func (s *Store) GrantItem(
	ctx context.Context,
	characterID int64,
	itemKey string,
	quantity int,
	eventKey,
	reason string,
) (int, error) {
	if quantity <= 0 {
		return 0, errors.New("grant quantity must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := recordEconomyEvent(ctx, tx, characterID, eventKey, "item_grant", itemKey, quantity, 0, reason); err != nil {
		return 0, err
	}
	var newQuantity int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO character_inventory (character_id, item_key, quantity)
		SELECT $1, item.key, $3
		FROM item_definitions item
		WHERE item.key = $2 AND $3 <= item.max_stack
		ON CONFLICT (character_id, item_key) DO UPDATE
		SET quantity = character_inventory.quantity + EXCLUDED.quantity,
		    updated_at = now()
		WHERE character_inventory.quantity + EXCLUDED.quantity <= (
			SELECT max_stack FROM item_definitions WHERE key = EXCLUDED.item_key
		)
		RETURNING quantity`,
		characterID,
		itemKey,
		quantity,
	).Scan(&newQuantity)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrInsufficient
	}
	if err != nil {
		return 0, fmt.Errorf("grant item: %w", err)
	}
	return newQuantity, tx.Commit()
}

func (s *Store) ConsumeItem(
	ctx context.Context,
	characterID int64,
	itemKey string,
	quantity int,
	reason string,
) (int, error) {
	if quantity <= 0 {
		return 0, errors.New("consume quantity must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var current int
	err = tx.QueryRowContext(ctx, `
		SELECT quantity FROM character_inventory
		WHERE character_id = $1 AND item_key = $2
		FOR UPDATE`,
		characterID,
		itemKey,
	).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) || current < quantity {
		return 0, ErrInsufficient
	}
	if err != nil {
		return 0, err
	}
	remaining := current - quantity
	if remaining == 0 {
		_, err = tx.ExecContext(ctx, `
			DELETE FROM character_inventory
			WHERE character_id = $1 AND item_key = $2`,
			characterID,
			itemKey,
		)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE character_inventory
			SET quantity = $3, updated_at = now()
			WHERE character_id = $1 AND item_key = $2`,
			characterID,
			itemKey,
			remaining,
		)
	}
	if err != nil {
		return 0, err
	}
	if err := recordEconomyEvent(ctx, tx, characterID, "", "item_consume", itemKey, -quantity, 0, reason); err != nil {
		return 0, err
	}
	return remaining, tx.Commit()
}
