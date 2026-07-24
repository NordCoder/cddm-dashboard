package supervisor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
)

func TestIssueBodyPersistsInProjectSnapshot(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	project, err := store.CreateProject(context.Background(), CreateProjectInput{
		Owner: "acme", Repository: "service", WorkflowMode: "pull_request", PollIntervalSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	if err := store.ReplaceSnapshot(context.Background(), project.ID, RepositorySnapshot{
		FetchedAt: now,
		Issues:    []Issue{{GitHubID: 101, Number: 11, Title: "Stage 4", Body: "authoritative body", State: "open", URL: "https://example.invalid/11", Author: "owner", CreatedAt: now, UpdatedAt: now}},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ProjectSnapshot(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Issues) != 1 || snapshot.Issues[0].Body != "authoritative body" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
