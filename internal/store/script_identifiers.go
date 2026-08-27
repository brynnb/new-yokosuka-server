package store

import (
	"context"
	"fmt"
)

// ScriptIdentifierCatalogEntry is an exact static symbol used by a currently
// published or recovered-reference script. UsageCount is informational; it is
// never used to infer runtime semantics or authorize an identifier.
type ScriptIdentifierCatalogEntry struct {
	Kind       string `json:"kind"`
	Identifier string `json:"identifier"`
	UsageCount int    `json:"usageCount"`
}

func (s *Store) ListPublishedScriptIdentifiers(ctx context.Context) ([]ScriptIdentifierCatalogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT identifier.kind, identifier.identifier, count(DISTINCT identifier.version_id)
		FROM script_version_identifiers identifier
		JOIN script_versions version ON version.id=identifier.version_id
		JOIN scripts script ON script.id=version.script_id
		WHERE script.archived_at IS NULL AND (
			version.id=script.current_published_version_id
			OR version.id=script.current_reference_version_id
		)
		GROUP BY identifier.kind,identifier.identifier
		ORDER BY identifier.kind,identifier.identifier`)
	if err != nil {
		return nil, fmt.Errorf("list published script identifiers: %w", err)
	}
	defer rows.Close()
	result := []ScriptIdentifierCatalogEntry{}
	for rows.Next() {
		var entry ScriptIdentifierCatalogEntry
		if err := rows.Scan(&entry.Kind, &entry.Identifier, &entry.UsageCount); err != nil {
			return nil, fmt.Errorf("scan published script identifier: %w", err)
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}
