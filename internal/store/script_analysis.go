package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

func copyScriptIndexes(ctx context.Context, tx *sql.Tx, fromVersionID, toVersionID int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO script_version_dependencies (version_id,access,kind,identifier)
		SELECT $2,access,kind,identifier
		FROM script_version_dependencies WHERE version_id=$1`, fromVersionID, toVersionID); err != nil {
		return fmt.Errorf("copy script dependencies: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO script_version_identifiers (version_id,kind,identifier)
		SELECT $2,kind,identifier
		FROM script_version_identifiers WHERE version_id=$1`, fromVersionID, toVersionID); err != nil {
		return fmt.Errorf("copy script identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO script_version_triggers (
			version_id,node_id,kind,area,actor,object_key,activity_key,priority,configuration
		)
		SELECT $2,node_id,kind,area,actor,object_key,activity_key,priority,configuration
		FROM script_version_triggers WHERE version_id=$1`, fromVersionID, toVersionID); err != nil {
		return fmt.Errorf("copy script triggers: %w", err)
	}
	return nil
}

func (s *Store) loadScriptAnalysis(ctx context.Context, versionID int64) (scriptcontent.Analysis, error) {
	analysis := scriptcontent.Analysis{
		Dependencies: []scriptcontent.Dependency{},
		Identifiers:  []scriptcontent.IdentifierUsage{},
		Triggers:     []scriptcontent.Trigger{},
		Warnings:     []string{},
	}
	dependencyRows, err := s.db.QueryContext(ctx, `
		SELECT access,kind,identifier
		FROM script_version_dependencies
		WHERE version_id=$1
		ORDER BY kind,identifier,access`, versionID)
	if err != nil {
		return scriptcontent.Analysis{}, fmt.Errorf("load script dependencies: %w", err)
	}
	for dependencyRows.Next() {
		var dependency scriptcontent.Dependency
		if err := dependencyRows.Scan(&dependency.Access, &dependency.Kind, &dependency.Identifier); err != nil {
			dependencyRows.Close()
			return scriptcontent.Analysis{}, fmt.Errorf("scan script dependency: %w", err)
		}
		analysis.Dependencies = append(analysis.Dependencies, dependency)
	}
	if err := dependencyRows.Close(); err != nil {
		return scriptcontent.Analysis{}, err
	}
	identifierRows, err := s.db.QueryContext(ctx, `
		SELECT kind,identifier FROM script_version_identifiers
		WHERE version_id=$1 ORDER BY kind,identifier`, versionID)
	if err != nil {
		return scriptcontent.Analysis{}, fmt.Errorf("load script identifiers: %w", err)
	}
	for identifierRows.Next() {
		var identifier scriptcontent.IdentifierUsage
		if err := identifierRows.Scan(&identifier.Kind, &identifier.Identifier); err != nil {
			identifierRows.Close()
			return scriptcontent.Analysis{}, fmt.Errorf("scan script identifier: %w", err)
		}
		analysis.Identifiers = append(analysis.Identifiers, identifier)
	}
	if err := identifierRows.Close(); err != nil {
		return scriptcontent.Analysis{}, err
	}
	triggerRows, err := s.db.QueryContext(ctx, `
		SELECT node_id,kind,COALESCE(area,''),COALESCE(actor,''),
		       COALESCE(object_key,''),COALESCE(activity_key,''),priority,configuration
		FROM script_version_triggers
		WHERE version_id=$1
		ORDER BY node_id`, versionID)
	if err != nil {
		return scriptcontent.Analysis{}, fmt.Errorf("load script triggers: %w", err)
	}
	defer triggerRows.Close()
	for triggerRows.Next() {
		var trigger scriptcontent.Trigger
		var configuration []byte
		if err := triggerRows.Scan(
			&trigger.NodeID, &trigger.Kind, &trigger.Area, &trigger.Actor,
			&trigger.Object, &trigger.Activity, &trigger.Priority, &configuration,
		); err != nil {
			return scriptcontent.Analysis{}, fmt.Errorf("scan script trigger: %w", err)
		}
		if err := json.Unmarshal(configuration, &trigger.Configuration); err != nil {
			return scriptcontent.Analysis{}, fmt.Errorf("decode script trigger: %w", err)
		}
		analysis.Triggers = append(analysis.Triggers, trigger)
	}
	return analysis, triggerRows.Err()
}
