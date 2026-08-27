package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrEmailTaken        = errors.New("email already taken")
	ErrNameTaken         = errors.New("character name already taken")
	ErrCharacterLimit    = errors.New("character limit reached")
	ErrInsufficient      = errors.New("insufficient balance or quantity")
	ErrDuplicateEvent    = errors.New("event already applied")
	ErrRevisionConflict  = errors.New("state revision conflict")
	ErrForbidden         = errors.New("forbidden")
	ErrScriptEventActive = errors.New("character already has an active script event")
	ErrScriptEventEnded  = errors.New("script event has ended")
	ErrAmbiguousTrigger  = errors.New("multiple published scripts have the same trigger priority")
)

const MaxCharactersPerAccount = 8

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	db             *sql.DB
	scriptCompiler scriptcontent.Compiler
}

func (s *Store) SetScriptCompiler(compiler scriptcontent.Compiler) {
	s.scriptCompiler = compiler
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7219042501)`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`,
			entry.Name(),
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		if applied {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`,
			entry.Name(),
		); err != nil {
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func HashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
