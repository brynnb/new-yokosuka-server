package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/game"
)

const characterColumns = `
	id, account_id, name, avatar_key, world_id, x, y, z, yaw,
	experience, current_hp, max_hp, yen, last_login_at, time_played_seconds,
	location_updated_at, created_at
`

func scanCharacter(scanner interface{ Scan(...any) error }) (Character, error) {
	var character Character
	var lastLogin, locationUpdated sql.NullTime
	err := scanner.Scan(
		&character.ID,
		&character.AccountID,
		&character.Name,
		&character.AvatarKey,
		&character.WorldID,
		&character.X,
		&character.Y,
		&character.Z,
		&character.Yaw,
		&character.Experience,
		&character.CurrentHP,
		&character.MaxHP,
		&character.Yen,
		&lastLogin,
		&character.TimePlayedSeconds,
		&locationUpdated,
		&character.CreatedAt,
	)
	if lastLogin.Valid {
		character.LastLoginAt = &lastLogin.Time
	}
	if locationUpdated.Valid {
		character.LocationUpdatedAt = &locationUpdated.Time
	}
	return character, err
}

func (s *Store) ListCharacters(ctx context.Context, accountID int64) ([]Character, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+characterColumns+`
		FROM characters
		WHERE account_id = $1 AND deleted_at IS NULL
		ORDER BY last_login_at DESC NULLS LAST, created_at ASC`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("list characters: %w", err)
	}
	defer rows.Close()
	characters := make([]Character, 0)
	for rows.Next() {
		character, err := scanCharacter(rows)
		if err != nil {
			return nil, fmt.Errorf("scan character: %w", err)
		}
		characters = append(characters, character)
	}
	return characters, rows.Err()
}

func (s *Store) CharacterForAccount(
	ctx context.Context,
	accountID,
	characterID int64,
) (Character, error) {
	character, err := scanCharacter(s.db.QueryRowContext(ctx, `
		SELECT `+characterColumns+`
		FROM characters
		WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
		characterID,
		accountID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Character{}, ErrNotFound
	}
	if err != nil {
		return Character{}, fmt.Errorf("load character: %w", err)
	}
	return character, nil
}

func (s *Store) CreateCharacter(
	ctx context.Context,
	accountID int64,
	name,
	avatarKey,
	worldID string,
	x,
	y,
	z,
	yaw float64,
	initialHP int,
) (Character, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Character{}, fmt.Errorf("begin character create: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM characters
		WHERE account_id = $1 AND deleted_at IS NULL`,
		accountID,
	).Scan(&count); err != nil {
		return Character{}, fmt.Errorf("count characters: %w", err)
	}
	if count >= MaxCharactersPerAccount {
		return Character{}, ErrCharacterLimit
	}
	character, err := scanCharacter(tx.QueryRowContext(ctx, `
		INSERT INTO characters
			(account_id, name, avatar_key, world_id, x, y, z, yaw, current_hp, max_hp, yen)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $10)
		RETURNING `+characterColumns,
		accountID,
		name,
		avatarKey,
		worldID,
		x,
		y,
		z,
		yaw,
		initialHP,
		game.StartingYen,
	))
	if isUniqueViolation(err) {
		return Character{}, ErrNameTaken
	}
	if err != nil {
		return Character{}, fmt.Errorf("insert character: %w", err)
	}
	if err := tx.Commit(); err != nil {
		if isUniqueViolation(err) {
			return Character{}, ErrNameTaken
		}
		return Character{}, fmt.Errorf("commit character create: %w", err)
	}
	return character, nil
}

func (s *Store) SoftDeleteCharacter(
	ctx context.Context,
	accountID,
	characterID int64,
) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE characters
		SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
		characterID,
		accountID,
	)
	if err != nil {
		return fmt.Errorf("delete character: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkCharacterLogin(ctx context.Context, characterID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE characters
		SET last_login_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`,
		characterID,
	)
	return err
}

func (s *Store) SaveLocation(
	ctx context.Context,
	characterID int64,
	location Location,
	playTime time.Duration,
) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE characters
		SET world_id = $2, x = $3, y = $4, z = $5, yaw = $6,
		    time_played_seconds = time_played_seconds + $7,
		    location_updated_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`,
		characterID,
		location.WorldID,
		location.X,
		location.Y,
		location.Z,
		location.Yaw,
		max(0, int64(playTime/time.Second)),
	)
	if err != nil {
		return fmt.Errorf("save location: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}
