package store

import (
	"io/fs"
	"testing"
)

func TestMigrationSetStartsFromSingleBaseline(t *testing.T) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var migrations []string
	for _, entry := range entries {
		if !entry.IsDir() {
			migrations = append(migrations, entry.Name())
		}
	}
	if len(migrations) != 1 || migrations[0] != "001_baseline.sql" {
		t.Fatalf("migration set = %v, want [001_baseline.sql]", migrations)
	}
}
