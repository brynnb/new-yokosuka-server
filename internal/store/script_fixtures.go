package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ScriptTestFixture is a named, reusable preview state. SourceVersion records
// the exact compiled version against which the fixture was authored; callers
// may deliberately run it against another visible version.
type ScriptTestFixture struct {
	ID                  int64           `json:"id"`
	ScriptID            int64           `json:"scriptId"`
	SourceVersionID     int64           `json:"sourceVersionId"`
	SourceVersionNumber int             `json:"sourceVersionNumber"`
	Origin              string          `json:"origin"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	StartNode           string          `json:"startNode"`
	Fixture             json.RawMessage `json:"fixture"`
	CreatedBy           *int64          `json:"createdBy,omitempty"`
	Revision            uint64          `json:"revision"`
	CreatedAt           time.Time       `json:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt"`
	ArchivedAt          *time.Time      `json:"archivedAt,omitempty"`
	ArchivedBy          *int64          `json:"archivedBy,omitempty"`
}

type ScriptTestFixtureInput struct {
	SourceVersionID int64
	Name            string
	Description     string
	StartNode       string
	Fixture         json.RawMessage
	Revision        uint64
}

const scriptTestFixtureColumns = `
	f.id, f.script_id, f.source_version_id, version.version,
	f.name, f.description, f.start_node, f.fixture, f.created_by, f.origin,
	f.revision, f.created_at, f.updated_at, f.archived_at, f.archived_by`

func scanScriptTestFixture(scanner interface{ Scan(...any) error }) (ScriptTestFixture, error) {
	var fixture ScriptTestFixture
	var createdBy, archivedBy sql.NullInt64
	var archivedAt sql.NullTime
	if err := scanner.Scan(
		&fixture.ID, &fixture.ScriptID, &fixture.SourceVersionID,
		&fixture.SourceVersionNumber, &fixture.Name, &fixture.Description,
		&fixture.StartNode, &fixture.Fixture, &createdBy, &fixture.Origin, &fixture.Revision,
		&fixture.CreatedAt, &fixture.UpdatedAt, &archivedAt, &archivedBy,
	); err != nil {
		return ScriptTestFixture{}, err
	}
	if createdBy.Valid {
		value := createdBy.Int64
		fixture.CreatedBy = &value
	}
	if archivedAt.Valid {
		value := archivedAt.Time
		fixture.ArchivedAt = &value
	}
	if archivedBy.Valid {
		value := archivedBy.Int64
		fixture.ArchivedBy = &value
	}
	return fixture, nil
}

func normalizedScriptTestFixture(input ScriptTestFixtureInput) ScriptTestFixtureInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.StartNode = strings.TrimSpace(input.StartNode)
	return input
}

func (s *Store) ListScriptTestFixtures(ctx context.Context, accountID, scriptID int64, includeArchived bool) ([]ScriptTestFixture, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+scriptTestFixtureColumns+`
		FROM script_test_fixtures f
		JOIN script_versions version ON version.id=f.source_version_id
		JOIN scripts script ON script.id=f.script_id
		LEFT JOIN script_collaborators collaborator
		  ON collaborator.script_id=script.id AND collaborator.account_id=NULLIF($1,0)
		LEFT JOIN accounts viewer ON viewer.id=NULLIF($1,0)
		WHERE f.script_id=$2
		  AND ($3 OR f.archived_at IS NULL)
		  AND (script.archived_at IS NULL OR collaborator.account_id IS NOT NULL
		       OR viewer.role IN ('moderator','admin'))
		  AND (collaborator.account_id IS NOT NULL
		       OR script.current_published_version_id IS NOT NULL
		       OR script.current_reference_version_id IS NOT NULL
		       OR viewer.role IN ('moderator','admin'))
		  AND (version.status IN ('published','reference','superseded')
		       OR collaborator.account_id IS NOT NULL
		       OR viewer.role IN ('moderator','admin'))
		ORDER BY f.archived_at NULLS FIRST, f.updated_at DESC, f.id DESC`,
		accountID, scriptID, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("list script test fixtures: %w", err)
	}
	defer rows.Close()
	fixtures := []ScriptTestFixture{}
	for rows.Next() {
		fixture, err := scanScriptTestFixture(rows)
		if err != nil {
			return nil, fmt.Errorf("scan script test fixture: %w", err)
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, rows.Err()
}

func (s *Store) CreateScriptTestFixture(ctx context.Context, accountID, scriptID int64, input ScriptTestFixtureInput) (ScriptTestFixture, error) {
	input = normalizedScriptTestFixture(input)
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO script_test_fixtures (
			script_id,source_version_id,name,description,start_node,fixture,created_by,origin
		)
		SELECT script.id,version.id,$4,$5,$6,$7,$1,'community'
		FROM scripts script
		JOIN script_versions version ON version.id=$3 AND version.script_id=script.id
		LEFT JOIN script_collaborators collaborator
		  ON collaborator.script_id=script.id AND collaborator.account_id=$1
		LEFT JOIN accounts viewer ON viewer.id=$1
		WHERE script.id=$2 AND script.archived_at IS NULL
		  AND viewer.account_type='registered'
		  AND version.content_format='yarn' AND version.compile_status='valid'
		  AND (version.status IN ('published','reference','superseded')
		       OR collaborator.account_id IS NOT NULL
		       OR viewer.role IN ('moderator','admin'))
		RETURNING id`, accountID, scriptID, input.SourceVersionID, input.Name,
		input.Description, input.StartNode, input.Fixture)
	var fixtureID int64
	if err := row.Scan(&fixtureID); errors.Is(err, sql.ErrNoRows) {
		return ScriptTestFixture{}, ErrForbidden
	} else if err != nil {
		return ScriptTestFixture{}, fmt.Errorf("create script test fixture: %w", err)
	}
	return s.scriptTestFixture(ctx, fixtureID)
}

func (s *Store) UpdateScriptTestFixture(ctx context.Context, accountID, scriptID, fixtureID int64, input ScriptTestFixtureInput) (ScriptTestFixture, error) {
	input = normalizedScriptTestFixture(input)
	row := s.db.QueryRowContext(ctx, `
		UPDATE script_test_fixtures f SET
			source_version_id=$5,name=$6,description=$7,start_node=$8,fixture=$9,
			revision=f.revision+1,updated_at=now()
		FROM scripts script, script_versions version, accounts viewer
		WHERE f.id=$3 AND f.script_id=$2 AND f.revision=$4
		  AND f.archived_at IS NULL AND script.id=f.script_id
		  AND script.archived_at IS NULL AND viewer.id=$1
		  AND version.id=$5 AND version.script_id=f.script_id
		  AND version.content_format='yarn' AND version.compile_status='valid'
		  AND (version.status IN ('published','reference','superseded')
		       OR EXISTS (SELECT 1 FROM script_collaborators visible_version
		          WHERE visible_version.script_id=f.script_id AND visible_version.account_id=$1)
		       OR viewer.role IN ('moderator','admin'))
		  AND (
			f.created_by=$1
			OR EXISTS (SELECT 1 FROM script_collaborators collaborator
				WHERE collaborator.script_id=f.script_id AND collaborator.account_id=$1
				AND collaborator.role IN ('owner','editor'))
			OR viewer.role IN ('moderator','admin')
		  )
		RETURNING f.id`, accountID, scriptID, fixtureID, input.Revision,
		input.SourceVersionID, input.Name, input.Description, input.StartNode, input.Fixture)
	var updatedID int64
	if err := row.Scan(&updatedID); errors.Is(err, sql.ErrNoRows) {
		return ScriptTestFixture{}, ErrRevisionConflict
	} else if err != nil {
		return ScriptTestFixture{}, fmt.Errorf("update script test fixture: %w", err)
	}
	return s.scriptTestFixture(ctx, updatedID)
}

func (s *Store) SetScriptTestFixtureArchived(ctx context.Context, accountID, scriptID, fixtureID int64, revision uint64, archived bool) (ScriptTestFixture, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE script_test_fixtures f SET
			archived_at=CASE WHEN $5 THEN now() ELSE NULL END,
			archived_by=CASE WHEN $5 THEN $1 ELSE NULL END,
			revision=f.revision+1,updated_at=now()
		FROM scripts script, accounts viewer
		WHERE f.id=$3 AND f.script_id=$2 AND f.revision=$4
		  AND script.id=f.script_id AND viewer.id=$1
		  AND (f.created_by=$1
			OR EXISTS (SELECT 1 FROM script_collaborators collaborator
				WHERE collaborator.script_id=f.script_id AND collaborator.account_id=$1
				AND collaborator.role IN ('owner','editor'))
			OR viewer.role IN ('moderator','admin'))
		  AND (($5 AND f.archived_at IS NULL) OR (NOT $5 AND f.archived_at IS NOT NULL))
		RETURNING f.id`, accountID, scriptID, fixtureID, revision, archived)
	var updatedID int64
	if err := row.Scan(&updatedID); errors.Is(err, sql.ErrNoRows) {
		return ScriptTestFixture{}, ErrRevisionConflict
	} else if err != nil {
		return ScriptTestFixture{}, fmt.Errorf("set script test fixture archived: %w", err)
	}
	return s.scriptTestFixture(ctx, updatedID)
}

func (s *Store) scriptTestFixture(ctx context.Context, fixtureID int64) (ScriptTestFixture, error) {
	fixture, err := scanScriptTestFixture(s.db.QueryRowContext(ctx, `
		SELECT `+scriptTestFixtureColumns+`
		FROM script_test_fixtures f
		JOIN script_versions version ON version.id=f.source_version_id
		WHERE f.id=$1`, fixtureID))
	if errors.Is(err, sql.ErrNoRows) {
		return ScriptTestFixture{}, ErrNotFound
	}
	return fixture, err
}
