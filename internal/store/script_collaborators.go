package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ScriptCollaborator struct {
	AccountID int64     `json:"accountId"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Store) ListScriptCollaborators(
	ctx context.Context,
	viewerAccountID, scriptID int64,
) ([]ScriptCollaborator, error) {
	var allowed bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM script_collaborators
			WHERE script_id=$1 AND account_id=$2
		)
	`, scriptID, viewerAccountID).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT collaborator.account_id,account.email,collaborator.role,collaborator.created_at
		FROM script_collaborators collaborator
		JOIN accounts account ON account.id=collaborator.account_id
		WHERE collaborator.script_id=$1
		ORDER BY CASE collaborator.role WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END,
		         lower(account.email),collaborator.account_id
	`, scriptID)
	if err != nil {
		return nil, fmt.Errorf("list script collaborators: %w", err)
	}
	defer rows.Close()
	result := []ScriptCollaborator{}
	for rows.Next() {
		var collaborator ScriptCollaborator
		if err := rows.Scan(
			&collaborator.AccountID, &collaborator.Email,
			&collaborator.Role, &collaborator.CreatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, collaborator)
	}
	return result, rows.Err()
}

func (s *Store) SetScriptCollaborator(
	ctx context.Context,
	ownerAccountID, scriptID int64,
	email, role string,
) (ScriptCollaborator, error) {
	email = normalizeEmail(email)
	role = strings.TrimSpace(role)
	if email == "" || (role != "editor" && role != "reviewer") {
		return ScriptCollaborator{}, errors.New("collaborator email and editor/reviewer role are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScriptCollaborator{}, err
	}
	defer tx.Rollback()
	var ownerRole, origin string
	var archivedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT collaborator.role,script.origin,script.archived_at
		FROM scripts script
		JOIN script_collaborators collaborator
		  ON collaborator.script_id=script.id AND collaborator.account_id=$2
		WHERE script.id=$1 FOR UPDATE OF script
	`, scriptID, ownerAccountID).Scan(&ownerRole, &origin, &archivedAt); errors.Is(err, sql.ErrNoRows) {
		return ScriptCollaborator{}, ErrForbidden
	} else if err != nil {
		return ScriptCollaborator{}, err
	}
	if ownerRole != "owner" || origin != "community" || archivedAt.Valid {
		return ScriptCollaborator{}, ErrForbidden
	}
	var targetAccountID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM accounts
		WHERE account_type='registered' AND lower(email)=lower($1)
	`, email).Scan(&targetAccountID); errors.Is(err, sql.ErrNoRows) {
		return ScriptCollaborator{}, ErrNotFound
	} else if err != nil {
		return ScriptCollaborator{}, err
	}
	if targetAccountID == ownerAccountID {
		return ScriptCollaborator{}, errors.New("the script owner role cannot be changed")
	}
	var previousRole sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT role FROM script_collaborators
		WHERE script_id=$1 AND account_id=$2
	`, scriptID, targetAccountID).Scan(&previousRole); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ScriptCollaborator{}, err
	}
	var collaborator ScriptCollaborator
	err = tx.QueryRowContext(ctx, `
		INSERT INTO script_collaborators AS collaborator (
			script_id,account_id,role
		) VALUES ($1,$2,$3)
		ON CONFLICT (script_id,account_id) DO UPDATE SET role=EXCLUDED.role
		WHERE collaborator.role <> 'owner'
		RETURNING account_id,$4,role,created_at
	`, scriptID, targetAccountID, role, email).Scan(
		&collaborator.AccountID, &collaborator.Email,
		&collaborator.Role, &collaborator.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptCollaborator{}, ErrForbidden
	}
	if err != nil {
		return ScriptCollaborator{}, fmt.Errorf("save script collaborator: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scripts SET updated_at=now() WHERE id=$1`, scriptID); err != nil {
		return ScriptCollaborator{}, err
	}
	action := "collaborator.added"
	details := map[string]any{
		"accountId": targetAccountID,
		"role":      role,
	}
	if previousRole.Valid {
		action = "collaborator.role-changed"
		details["previousRole"] = previousRole.String
	}
	if err := insertScriptModerationEvent(
		ctx, tx, scriptID, nil, ownerAccountID, action, details,
	); err != nil {
		return ScriptCollaborator{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScriptCollaborator{}, err
	}
	return collaborator, nil
}

func (s *Store) RemoveScriptCollaborator(
	ctx context.Context,
	ownerAccountID, scriptID, collaboratorAccountID int64,
) error {
	if collaboratorAccountID <= 0 || collaboratorAccountID == ownerAccountID {
		return ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ownerRole, origin string
	var archivedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT collaborator.role,script.origin,script.archived_at
		FROM scripts script
		JOIN script_collaborators collaborator
		  ON collaborator.script_id=script.id AND collaborator.account_id=$2
		WHERE script.id=$1 FOR UPDATE OF script
	`, scriptID, ownerAccountID).Scan(&ownerRole, &origin, &archivedAt); errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	} else if err != nil {
		return err
	}
	if ownerRole != "owner" || origin != "community" || archivedAt.Valid {
		return ErrForbidden
	}
	var removedRole string
	err = tx.QueryRowContext(ctx, `
		DELETE FROM script_collaborators
		WHERE script_id=$1 AND account_id=$2 AND role <> 'owner'
		RETURNING role
	`, scriptID, collaboratorAccountID).Scan(&removedRole)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("remove script collaborator: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scripts SET updated_at=now() WHERE id=$1`, scriptID); err != nil {
		return err
	}
	if err := insertScriptModerationEvent(
		ctx, tx, scriptID, nil, ownerAccountID, "collaborator.removed",
		map[string]any{"accountId": collaboratorAccountID, "role": removedRole},
	); err != nil {
		return err
	}
	return tx.Commit()
}
