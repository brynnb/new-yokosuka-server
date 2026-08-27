package store

import (
	"context"
	"fmt"
	"time"
)

type AdminGrowthEvents struct {
	Users      []time.Time
	Characters []time.Time
	LastLogins []time.Time
}

func (s *Store) AdminGrowthEvents(ctx context.Context) (AdminGrowthEvents, error) {
	read := func(query string) ([]time.Time, error) {
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		values := []time.Time{}
		for rows.Next() {
			var value time.Time
			if err := rows.Scan(&value); err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, rows.Err()
	}

	users, err := read(`SELECT created_at FROM accounts`)
	if err != nil {
		return AdminGrowthEvents{}, fmt.Errorf("load account growth events: %w", err)
	}
	characters, err := read(`SELECT created_at FROM characters WHERE deleted_at IS NULL`)
	if err != nil {
		return AdminGrowthEvents{}, fmt.Errorf("load character growth events: %w", err)
	}
	lastLogins, err := read(`
		SELECT MAX(last_login_at)
		FROM characters
		WHERE deleted_at IS NULL AND last_login_at IS NOT NULL
		GROUP BY account_id`)
	if err != nil {
		return AdminGrowthEvents{}, fmt.Errorf("load player activity: %w", err)
	}
	return AdminGrowthEvents{Users: users, Characters: characters, LastLogins: lastLogins}, nil
}
