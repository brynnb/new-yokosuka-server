package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxScriptReviewCommentLength = 4000

type ScriptReviewComment struct {
	ID        int64     `json:"id"`
	AuthorID  int64     `json:"authorId"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type ScriptReviewThread struct {
	ID         int64                 `json:"id"`
	VersionID  int64                 `json:"versionId"`
	LineNumber *int                  `json:"lineNumber,omitempty"`
	CreatedBy  int64                 `json:"createdBy"`
	Status     string                `json:"status"`
	ResolvedBy *int64                `json:"resolvedBy,omitempty"`
	ResolvedAt *time.Time            `json:"resolvedAt,omitempty"`
	CreatedAt  time.Time             `json:"createdAt"`
	UpdatedAt  time.Time             `json:"updatedAt"`
	Comments   []ScriptReviewComment `json:"comments"`
}

func normalizeScriptReviewBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > maxScriptReviewCommentLength {
		return "", errors.New("review comment must contain between 1 and 4000 characters")
	}
	return body, nil
}

func scanScriptReviewThread(scanner interface{ Scan(...any) error }) (ScriptReviewThread, error) {
	var thread ScriptReviewThread
	var lineNumber sql.NullInt32
	var resolvedBy sql.NullInt64
	var resolvedAt sql.NullTime
	err := scanner.Scan(
		&thread.ID, &thread.VersionID, &lineNumber, &thread.CreatedBy,
		&thread.Status, &resolvedBy, &resolvedAt, &thread.CreatedAt, &thread.UpdatedAt,
	)
	if lineNumber.Valid {
		value := int(lineNumber.Int32)
		thread.LineNumber = &value
	}
	if resolvedBy.Valid {
		value := resolvedBy.Int64
		thread.ResolvedBy = &value
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time
		thread.ResolvedAt = &value
	}
	return thread, err
}

const scriptReviewThreadColumns = `
	thread.id,thread.version_id,thread.line_number,thread.created_by,
	thread.status,thread.resolved_by,thread.resolved_at,
	thread.created_at,thread.updated_at`

func (s *Store) ListScriptReviewThreads(
	ctx context.Context,
	viewerAccountID, scriptID int64,
	versionNumber int,
) ([]ScriptReviewThread, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+scriptReviewThreadColumns+`
		FROM script_review_threads thread
		JOIN script_versions version ON version.id=thread.version_id
		JOIN scripts script ON script.id=version.script_id
		LEFT JOIN script_collaborators collaborator
		  ON collaborator.script_id=script.id AND collaborator.account_id=NULLIF($1,0)
		LEFT JOIN accounts viewer ON viewer.id=NULLIF($1,0)
		WHERE script.id=$2 AND version.version=$3 AND script.origin='community'
		  AND (version.status IN ('published','superseded')
		       OR collaborator.account_id IS NOT NULL
		       OR viewer.role IN ('moderator','admin'))
		  AND (script.archived_at IS NULL
		       OR collaborator.account_id IS NOT NULL
		       OR viewer.role IN ('moderator','admin'))
		ORDER BY CASE thread.status WHEN 'open' THEN 0 ELSE 1 END,
		         thread.created_at,thread.id
	`, viewerAccountID, scriptID, versionNumber)
	if err != nil {
		return nil, fmt.Errorf("list script review threads: %w", err)
	}
	defer rows.Close()
	threads := []ScriptReviewThread{}
	for rows.Next() {
		thread, err := scanScriptReviewThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		var visible bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM script_versions version
				JOIN scripts script ON script.id=version.script_id
				LEFT JOIN script_collaborators collaborator
				  ON collaborator.script_id=script.id AND collaborator.account_id=NULLIF($1,0)
				LEFT JOIN accounts viewer ON viewer.id=NULLIF($1,0)
				WHERE script.id=$2 AND version.version=$3 AND script.origin='community'
				  AND (version.status IN ('published','superseded')
				       OR collaborator.account_id IS NOT NULL
				       OR viewer.role IN ('moderator','admin'))
				  AND (script.archived_at IS NULL
				       OR collaborator.account_id IS NOT NULL
				       OR viewer.role IN ('moderator','admin'))
			)
		`, viewerAccountID, scriptID, versionNumber).Scan(&visible); err != nil {
			return nil, err
		}
		if !visible {
			return nil, ErrNotFound
		}
	}
	for index := range threads {
		comments, err := s.listScriptReviewComments(ctx, threads[index].ID)
		if err != nil {
			return nil, err
		}
		threads[index].Comments = comments
	}
	return threads, nil
}

func (s *Store) listScriptReviewComments(ctx context.Context, threadID int64) ([]ScriptReviewComment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,author_id,body,created_at
		FROM script_review_comments
		WHERE thread_id=$1 ORDER BY created_at,id
	`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := []ScriptReviewComment{}
	for rows.Next() {
		var comment ScriptReviewComment
		if err := rows.Scan(&comment.ID, &comment.AuthorID, &comment.Body, &comment.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

func (s *Store) CreateScriptReviewThread(
	ctx context.Context,
	accountID, scriptID int64,
	versionNumber int,
	lineNumber *int,
	body string,
) (ScriptReviewThread, error) {
	body, err := normalizeScriptReviewBody(body)
	if err != nil {
		return ScriptReviewThread{}, err
	}
	if lineNumber != nil && *lineNumber <= 0 {
		return ScriptReviewThread{}, errors.New("review line number must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptReviewThread{}, err
	}
	defer tx.Rollback()
	var versionID int64
	var sourceText string
	err = tx.QueryRowContext(ctx, `
		SELECT version.id,version.source_text
		FROM script_versions version
		JOIN scripts script ON script.id=version.script_id
		LEFT JOIN script_collaborators collaborator
		  ON collaborator.script_id=script.id AND collaborator.account_id=$1
		JOIN accounts viewer ON viewer.id=$1 AND viewer.account_type='registered'
		WHERE script.id=$2 AND version.version=$3 AND script.origin='community'
		  AND version.content_format='yarn' AND version.source_text IS NOT NULL
		  AND version.status IN ('review','published','superseded')
		  AND script.archived_at IS NULL
		  AND (version.status IN ('published','superseded')
		       OR collaborator.account_id IS NOT NULL
		       OR viewer.role IN ('moderator','admin'))
	`, accountID, scriptID, versionNumber).Scan(&versionID, &sourceText)
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptReviewThread{}, ErrForbidden
	}
	if err != nil {
		return ScriptReviewThread{}, err
	}
	if lineNumber != nil {
		lineCount := strings.Count(sourceText, "\n")
		if sourceText != "" && !strings.HasSuffix(sourceText, "\n") {
			lineCount++
		}
		if *lineNumber > lineCount {
			return ScriptReviewThread{}, errors.New("review line number is outside the saved Yarn source")
		}
	}
	thread, err := scanScriptReviewThread(tx.QueryRowContext(ctx, `
		INSERT INTO script_review_threads (version_id,line_number,created_by)
		VALUES ($1,$2,$3) RETURNING
			id,version_id,line_number,created_by,status,resolved_by,resolved_at,created_at,updated_at
	`, versionID, lineNumber, accountID,
	))
	if err != nil {
		return ScriptReviewThread{}, fmt.Errorf("create script review thread: %w", err)
	}
	var comment ScriptReviewComment
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO script_review_comments (thread_id,author_id,body)
		VALUES ($1,$2,$3) RETURNING id,author_id,body,created_at
	`, thread.ID, accountID, body).Scan(
		&comment.ID, &comment.AuthorID, &comment.Body, &comment.CreatedAt,
	); err != nil {
		return ScriptReviewThread{}, fmt.Errorf("create script review comment: %w", err)
	}
	thread.Comments = []ScriptReviewComment{comment}
	if err := tx.Commit(); err != nil {
		return ScriptReviewThread{}, err
	}
	return thread, nil
}

func (s *Store) AddScriptReviewComment(
	ctx context.Context,
	accountID, scriptID, threadID int64,
	body string,
) (ScriptReviewComment, error) {
	body, err := normalizeScriptReviewBody(body)
	if err != nil {
		return ScriptReviewComment{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptReviewComment{}, err
	}
	defer tx.Rollback()
	var comment ScriptReviewComment
	err = tx.QueryRowContext(ctx, `
		INSERT INTO script_review_comments (thread_id,author_id,body)
		SELECT thread.id,$1,$4
		FROM script_review_threads thread
		JOIN script_versions version ON version.id=thread.version_id
		JOIN scripts script ON script.id=version.script_id
		LEFT JOIN script_collaborators collaborator
		  ON collaborator.script_id=script.id AND collaborator.account_id=$1
		JOIN accounts viewer ON viewer.id=$1 AND viewer.account_type='registered'
		WHERE script.id=$2 AND thread.id=$3 AND script.origin='community'
		  AND version.status IN ('review','published','superseded')
		  AND script.archived_at IS NULL
		  AND (version.status IN ('published','superseded')
		       OR collaborator.account_id IS NOT NULL
		       OR viewer.role IN ('moderator','admin'))
		RETURNING id,author_id,body,created_at
	`, accountID, scriptID, threadID, body).Scan(
		&comment.ID, &comment.AuthorID, &comment.Body, &comment.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptReviewComment{}, ErrForbidden
	}
	if err != nil {
		return ScriptReviewComment{}, fmt.Errorf("add script review comment: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE script_review_threads SET updated_at=now() WHERE id=$1`, threadID); err != nil {
		return ScriptReviewComment{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptReviewComment{}, err
	}
	return comment, nil
}

func (s *Store) SetScriptReviewThreadResolved(
	ctx context.Context,
	accountID, scriptID, threadID int64,
	resolved bool,
) (ScriptReviewThread, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptReviewThread{}, err
	}
	defer tx.Rollback()
	var thread ScriptReviewThread
	err = tx.QueryRowContext(ctx, `
		UPDATE script_review_threads thread SET
			status=CASE WHEN $4 THEN 'resolved' ELSE 'open' END,
			resolved_by=CASE WHEN $4 THEN $1 ELSE NULL END,
			resolved_at=CASE WHEN $4 THEN now() ELSE NULL END,
			updated_at=now()
		FROM script_versions version,scripts script,accounts viewer
		WHERE thread.id=$3 AND version.id=thread.version_id
		  AND thread.status <> CASE WHEN $4 THEN 'resolved' ELSE 'open' END
		  AND script.id=version.script_id AND script.id=$2
		  AND viewer.id=$1 AND viewer.account_type='registered'
		  AND script.origin='community' AND script.archived_at IS NULL
		  AND (thread.created_by=$1
		       OR viewer.role IN ('moderator','admin')
		       OR EXISTS (SELECT 1 FROM script_collaborators collaborator
		          WHERE collaborator.script_id=script.id AND collaborator.account_id=$1))
		RETURNING `+scriptReviewThreadColumns,
		accountID, scriptID, threadID, resolved,
	).Scan(
		&thread.ID, &thread.VersionID, new(sql.NullInt32), &thread.CreatedBy,
		&thread.Status, new(sql.NullInt64), new(sql.NullTime), &thread.CreatedAt, &thread.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptReviewThread{}, ErrForbidden
	}
	if err != nil {
		return ScriptReviewThread{}, fmt.Errorf("resolve script review thread: %w", err)
	}
	action := "review-thread.reopened"
	if resolved {
		action = "review-thread.resolved"
	}
	if err := insertScriptModerationEvent(
		ctx, tx, scriptID, &thread.VersionID, accountID, action,
		map[string]any{"threadId": threadID},
	); err != nil {
		return ScriptReviewThread{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptReviewThread{}, err
	}
	// Reload through the shared scanner so nullable line and resolution fields
	// cannot diverge from the persisted row.
	thread, err = scanScriptReviewThread(s.db.QueryRowContext(ctx, `
		SELECT `+scriptReviewThreadColumns+` FROM script_review_threads thread WHERE thread.id=$1
	`, threadID))
	if err != nil {
		return ScriptReviewThread{}, err
	}
	thread.Comments, err = s.listScriptReviewComments(ctx, threadID)
	return thread, err
}
