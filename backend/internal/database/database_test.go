package database

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenCreatesDatabaseAndAppliesMigrations(t *testing.T) {
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
	if version != 4 {
		t.Fatalf("user_version = %d, want 4", version)
	}

	for _, table := range []string{
		"projects", "github_issues", "github_issue_comments", "github_pull_requests", "github_ci_summaries",
		"planning_generations", "model_invocations", "prompt_plans", "policy_decisions",
	} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("read table %q: %v", table, err)
		}
	}
	var bodyColumn string
	if err := db.QueryRow(`SELECT name FROM pragma_table_info('github_issues') WHERE name = 'body'`).Scan(&bodyColumn); err != nil {
		t.Fatalf("read github_issues.body column: %v", err)
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
	if err := second.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 4 {
		t.Fatalf("migration count = %d, want 4", count)
	}
}
