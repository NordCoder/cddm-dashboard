package database

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenCreatesDatabaseAndAppliesInitialMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cddm.db")

	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Fatalf("user_version = %d, want 1", version)
	}

	var migrationName string
	if err := db.QueryRow("SELECT name FROM schema_migrations WHERE version = 1").Scan(&migrationName); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if migrationName != "initial" {
		t.Fatalf("migration name = %q, want initial", migrationName)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cddm.db")

	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer second.Close()

	var count int
	if err := second.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration count = %d, want 1", count)
	}
}
