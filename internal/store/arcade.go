package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type ArcadeHighScore struct {
	MachineID  string    `json:"machineId"`
	Score      float64   `json:"score"`
	AchievedAt time.Time `json:"achievedAt"`
}

type ArcadeScoreSubmission struct {
	ArcadeHighScore
	NewHighScore bool `json:"newHighScore"`
}

func (s *Store) ArcadeHighScores(
	ctx context.Context,
) ([]ArcadeHighScore, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT machine_id, score, achieved_at
		FROM arcade_high_scores
		ORDER BY machine_id`)
	if err != nil {
		return nil, fmt.Errorf("list arcade high scores: %w", err)
	}
	defer rows.Close()
	scores := make([]ArcadeHighScore, 0, 4)
	for rows.Next() {
		var score ArcadeHighScore
		if err := rows.Scan(
			&score.MachineID,
			&score.Score,
			&score.AchievedAt,
		); err != nil {
			return nil, fmt.Errorf("scan arcade high score: %w", err)
		}
		scores = append(scores, score)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate arcade high scores: %w", err)
	}
	return scores, nil
}

func (s *Store) SubmitArcadeScore(
	ctx context.Context,
	accountID int64,
	characterID int64,
	machineID string,
	score float64,
) (ArcadeScoreSubmission, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ArcadeScoreSubmission{}, fmt.Errorf("begin arcade score: %w", err)
	}
	defer tx.Rollback()
	var result ArcadeScoreSubmission
	err = tx.QueryRowContext(ctx, `
		SELECT machine_id, score, achieved_at
		FROM arcade_high_scores
		WHERE machine_id = $1
		FOR UPDATE`,
		machineID,
	).Scan(
		&result.MachineID,
		&result.Score,
		&result.AchievedAt,
	)
	if err == sql.ErrNoRows {
		return ArcadeScoreSubmission{}, ErrNotFound
	}
	if err != nil {
		return ArcadeScoreSubmission{}, fmt.Errorf("lock arcade score: %w", err)
	}
	if score > result.Score {
		err = tx.QueryRowContext(ctx, `
			UPDATE arcade_high_scores
			SET score = $2,
			    account_id = $3,
			    character_id = $4,
			    achieved_at = now()
			WHERE machine_id = $1
			RETURNING score, achieved_at`,
			machineID,
			score,
			accountID,
			characterID,
		).Scan(&result.Score, &result.AchievedAt)
		if err != nil {
			return ArcadeScoreSubmission{}, fmt.Errorf("update arcade score: %w", err)
		}
		result.NewHighScore = true
	}
	if err := tx.Commit(); err != nil {
		return ArcadeScoreSubmission{}, fmt.Errorf("commit arcade score: %w", err)
	}
	return result, nil
}
