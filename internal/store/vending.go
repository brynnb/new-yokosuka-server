package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type VendingPurchase struct {
	RequestID  string          `json:"requestId"`
	MachineID  string          `json:"machineId"`
	DrinkKey   string          `json:"drinkKey"`
	Price      int64           `json:"price"`
	WinningCan bool            `json:"winningCan"`
	Yen        int64           `json:"yen"`
	Inventory  []InventoryItem `json:"inventory"`
}

func scanVendingPurchase(
	scanner interface{ Scan(...any) error },
) (VendingPurchase, error) {
	var purchase VendingPurchase
	err := scanner.Scan(
		&purchase.RequestID,
		&purchase.MachineID,
		&purchase.DrinkKey,
		&purchase.Price,
		&purchase.WinningCan,
	)
	return purchase, err
}

func vendingPurchaseForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	characterID int64,
	requestID string,
) (VendingPurchase, error) {
	return scanVendingPurchase(tx.QueryRowContext(ctx, `
		SELECT request_id, machine_id, drink_key, price, winning_can
		FROM vending_purchases
		WHERE character_id = $1 AND request_id = $2
		FOR UPDATE`,
		characterID,
		requestID,
	))
}

func populateVendingState(
	ctx context.Context,
	tx *sql.Tx,
	characterID int64,
	purchase *VendingPurchase,
) error {
	if err := tx.QueryRowContext(ctx, `
		SELECT yen
		FROM characters
		WHERE id = $1 AND deleted_at IS NULL`,
		characterID,
	).Scan(&purchase.Yen); err != nil {
		return fmt.Errorf("load vending balance: %w", err)
	}
	inventory, err := inventoryWithQuerier(ctx, tx, characterID)
	if err != nil {
		return err
	}
	purchase.Inventory = inventory
	return nil
}

func (s *Store) PurchaseVendingDrink(
	ctx context.Context,
	characterID int64,
	requestID,
	machineID,
	drinkKey string,
	price int64,
	winningCan bool,
) (VendingPurchase, error) {
	requestID = strings.TrimSpace(requestID)
	machineID = strings.TrimSpace(machineID)
	drinkKey = strings.TrimSpace(drinkKey)
	if characterID <= 0 || requestID == "" || machineID == "" ||
		drinkKey == "" || price <= 0 {
		return VendingPurchase{}, errors.New("invalid vending purchase")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return VendingPurchase{}, err
	}
	defer tx.Rollback()

	existing, err := vendingPurchaseForUpdate(
		ctx,
		tx,
		characterID,
		requestID,
	)
	if err == nil {
		if existing.MachineID != machineID || existing.DrinkKey != drinkKey {
			return VendingPurchase{}, ErrDuplicateEvent
		}
		if err := populateVendingState(
			ctx,
			tx,
			characterID,
			&existing,
		); err != nil {
			return VendingPurchase{}, err
		}
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return VendingPurchase{}, fmt.Errorf("lookup vending purchase: %w", err)
	}

	var balance int64
	err = tx.QueryRowContext(ctx, `
		UPDATE characters
		SET yen = yen - $2, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND yen >= $2
		RETURNING yen`,
		characterID,
		price,
	).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return VendingPurchase{}, ErrInsufficient
	}
	if err != nil {
		return VendingPurchase{}, fmt.Errorf("debit vending purchase: %w", err)
	}

	quantityDelta := 0
	itemKey := ""
	if winningCan {
		itemKey = "winning_can"
		err = tx.QueryRowContext(ctx, `
			INSERT INTO character_inventory (character_id, item_key, quantity)
			VALUES ($1, 'winning_can', 1)
			ON CONFLICT (character_id, item_key) DO UPDATE
			SET quantity = character_inventory.quantity + 1,
			    updated_at = now()
			WHERE character_inventory.quantity < (
				SELECT max_stack
				FROM item_definitions
				WHERE key = 'winning_can'
			)
			RETURNING quantity`,
			characterID,
		).Scan(&quantityDelta)
		if errors.Is(err, sql.ErrNoRows) {
			// The ordinary selected drink still dispenses when the player's
			// Winning Can stack is full. Do not charge for an undeliverable
			// prize or abort an otherwise valid Shenmue purchase.
			winningCan = false
			itemKey = ""
			quantityDelta = 0
		} else if err != nil {
			return VendingPurchase{}, fmt.Errorf("grant winning can: %w", err)
		} else {
			quantityDelta = 1
		}
	}

	purchase := VendingPurchase{
		RequestID: requestID, MachineID: machineID, DrinkKey: drinkKey,
		Price: price, WinningCan: winningCan,
	}
	purchase, err = scanVendingPurchase(tx.QueryRowContext(ctx, `
		INSERT INTO vending_purchases
			(character_id, request_id, machine_id, drink_key, price, winning_can)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING request_id, machine_id, drink_key, price, winning_can`,
		characterID,
		requestID,
		machineID,
		drinkKey,
		price,
		winningCan,
	))
	if isUniqueViolation(err) {
		return VendingPurchase{}, ErrDuplicateEvent
	}
	if err != nil {
		return VendingPurchase{}, fmt.Errorf("record vending purchase: %w", err)
	}
	if err := recordEconomyEvent(
		ctx,
		tx,
		characterID,
		"vending:"+requestID,
		"vending_purchase",
		itemKey,
		quantityDelta,
		-price,
		"vending machine "+machineID+": "+drinkKey,
	); err != nil {
		return VendingPurchase{}, fmt.Errorf("record vending economy event: %w", err)
	}
	purchase.Yen = balance
	inventory, err := inventoryWithQuerier(ctx, tx, characterID)
	if err != nil {
		return VendingPurchase{}, err
	}
	purchase.Inventory = inventory
	if err := tx.Commit(); err != nil {
		return VendingPurchase{}, err
	}
	return purchase, nil
}
