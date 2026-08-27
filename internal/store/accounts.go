package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func scanAccount(scanner interface{ Scan(...any) error }) (Account, error) {
	var account Account
	var email sql.NullString
	err := scanner.Scan(
		&account.ID,
		&account.AccountType,
		&account.Role,
		&email,
		&account.CreatedAt,
	)
	if email.Valid {
		account.Email = email.String
	}
	return account, err
}

func (s *Store) GetOrCreateGuest(ctx context.Context, guestTokenHash string) (Account, error) {
	account, err := scanAccount(s.db.QueryRowContext(ctx, `
		INSERT INTO accounts (account_type, guest_token_hash)
		VALUES ('guest', $1)
		ON CONFLICT (guest_token_hash) WHERE guest_token_hash IS NOT NULL
		DO UPDATE SET updated_at = accounts.updated_at
		RETURNING id, account_type, role, email, created_at`,
		guestTokenHash,
	))
	if err != nil {
		return Account{}, fmt.Errorf("get or create guest: %w", err)
	}
	return account, nil
}

func (s *Store) CreateRegisteredAccount(
	ctx context.Context,
	email,
	passwordHash string,
) (Account, error) {
	account, err := scanAccount(s.db.QueryRowContext(ctx, `
		INSERT INTO accounts (account_type, email, password_hash)
		VALUES ('registered', $1, $2)
		RETURNING id, account_type, role, email, created_at`,
		normalizeEmail(email),
		passwordHash,
	))
	if isUniqueViolation(err) {
		return Account{}, ErrEmailTaken
	}
	if err != nil {
		return Account{}, fmt.Errorf("create account: %w", err)
	}
	return account, nil
}

func (s *Store) UpgradeGuestAccount(
	ctx context.Context,
	accountID int64,
	email,
	passwordHash string,
) (Account, error) {
	account, err := scanAccount(s.db.QueryRowContext(ctx, `
		UPDATE accounts
		SET account_type = 'registered',
		    email = $2,
		    password_hash = $3,
		    guest_token_hash = NULL,
		    updated_at = now()
		WHERE id = $1
		  AND account_type = 'guest'
		RETURNING id, account_type, role, email, created_at`,
		accountID,
		normalizeEmail(email),
		passwordHash,
	))
	if isUniqueViolation(err) {
		return Account{}, ErrEmailTaken
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("upgrade guest account: %w", err)
	}
	return account, nil
}

func (s *Store) AccountCredentials(
	ctx context.Context,
	email string,
) (Account, string, error) {
	var account Account
	var passwordHash string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, account_type, role, email, created_at, password_hash
		FROM accounts
		WHERE lower(email) = lower($1)
		  AND account_type = 'registered'`,
		normalizeEmail(email),
	).Scan(
		&account.ID,
		&account.AccountType,
		&account.Role,
		&account.Email,
		&account.CreatedAt,
		&passwordHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, "", ErrNotFound
	}
	if err != nil {
		return Account{}, "", fmt.Errorf("load account credentials: %w", err)
	}
	return account, passwordHash, nil
}

func (s *Store) CreateSession(
	ctx context.Context,
	tokenHash string,
	accountID int64,
	expiresAt time.Time,
) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO account_sessions (token_hash, account_id, expires_at)
		VALUES ($1, $2, $3)`,
		tokenHash,
		accountID,
		expiresAt,
	); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) AccountBySession(
	ctx context.Context,
	tokenHash string,
) (Account, error) {
	account, err := scanAccount(s.db.QueryRowContext(ctx, `
		SELECT a.id, a.account_type, a.role, a.email, a.created_at
		FROM account_sessions session
		JOIN accounts a ON a.id = session.account_id
		WHERE session.token_hash = $1
		  AND session.revoked_at IS NULL
		  AND session.expires_at > now()`,
		tokenHash,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("load session: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE account_sessions SET last_seen_at = now() WHERE token_hash = $1`,
		tokenHash,
	)
	return account, nil
}

func (s *Store) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE account_sessions
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash,
	)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM account_sessions
		WHERE expires_at <= now() OR revoked_at < now() - interval '7 days'`)
	return err
}
