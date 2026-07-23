package supervisor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
)

type fakeGitHubClient struct {
	mu        sync.Mutex
	snapshots map[string]RepositorySnapshot
	errors    map[string]error
	calls     map[string]int
}

func (f *fakeGitHubClient) Snapshot(_ context.Context, owner, repository string) (RepositorySnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := owner + "/" + repository
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[key]++
	if err := f.errors[key]; err != nil {
		return RepositorySnapshot{}, err
	}
	return f.snapshots[key], nil
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func createTestProject(t *testing.T, store *Store, owner, repository string, polling bool, interval int64) Project {
	t.Helper()
	project, err := store.CreateProject(context.Background(), CreateProjectInput{
		Owner: owner, Repository: repository, WorkflowMode: "pull_request",
		PollingEnabled: polling, PollIntervalSeconds: interval,
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project
}

func snapshotAt(now time.Time, issueID int64, issueNumber int, head string) RepositorySnapshot {
	return RepositorySnapshot{
		FetchedAt: now,
		Issues: []Issue{{
			GitHubID: issueID, Number: issueNumber, Title: fmt.Sprintf("Issue %d", issueNumber),
			State: "open", URL: fmt.Sprintf("https://example/issues/%d", issueNumber), Author: "owner",
			CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			Labels: []Label{{Name: "stage", Color: "ffffff"}},
			Comments: []Comment{{
				GitHubID: issueID * 10, Body: "comment", Author: "worker",
				URL: "https://example/comment", CreatedAt: now, UpdatedAt: now,
			}},
			PullRequests: []PullRequest{{
				GitHubID: issueID * 100, Number: issueNumber + 100, Title: "Candidate", State: "open",
				Draft: true, MergeableState: "clean", BaseRef: "main", HeadRef: "candidate", HeadSHA: head,
				URL: "https://example/pr", UpdatedAt: now,
				CI: CISummary{HeadSHA: head, Status: "completed", Conclusion: "success", Source: "check_runs", UpdatedAt: now},
			}},
		}},
	}
}

func TestStoreKeepsProjectsIsolatedAndDeletionCascades(t *testing.T) {
	store := openTestStore(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	first := createTestProject(t, store, "one", "repo", true, 60)
	second := createTestProject(t, store, "two", "repo", false, 120)

	if err := store.ReplaceSnapshot(context.Background(), first.ID, snapshotAt(now, 11, 1, "head-one")); err != nil {
		t.Fatalf("ReplaceSnapshot(first) error = %v", err)
	}
	if err := store.ReplaceSnapshot(context.Background(), second.ID, snapshotAt(now, 22, 2, "head-two")); err != nil {
		t.Fatalf("ReplaceSnapshot(second) error = %v", err)
	}

	workspace, err := store.WorkspaceSnapshot(context.Background())
	if err != nil {
		t.Fatalf("WorkspaceSnapshot() error = %v", err)
	}
	if len(workspace.Projects) != 2 {
		t.Fatalf("project count = %d, want 2", len(workspace.Projects))
	}
	for _, project := range workspace.Projects {
		if len(project.Issues) != 1 {
			t.Fatalf("project %d issue count = %d", project.Project.ID, len(project.Issues))
		}
		if project.Project.ID == first.ID && project.Issues[0].GitHubID != 11 {
			t.Fatalf("first project leaked data: %#v", project.Issues[0])
		}
		if project.Project.ID == second.ID && project.Issues[0].GitHubID != 22 {
			t.Fatalf("second project leaked data: %#v", project.Issues[0])
		}
	}

	if err := store.DeleteProject(context.Background(), first.ID); err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}
	if _, err := store.ProjectSnapshot(context.Background(), first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted ProjectSnapshot() error = %v, want ErrNotFound", err)
	}
	for _, table := range []string{"github_issues", "github_issue_labels", "github_issue_comments", "github_pull_requests", "github_ci_summaries"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE project_id = ?`, first.ID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d deleted project rows", table, count)
		}
	}
	remaining, err := store.ProjectSnapshot(context.Background(), second.ID)
	if err != nil || len(remaining.Issues) != 1 || remaining.Issues[0].GitHubID != 22 {
		t.Fatalf("remaining project = %#v, error = %v", remaining, err)
	}
}

func TestSyncIsIdempotentAndUpdatesChangedHead(t *testing.T) {
	store := openTestStore(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	project := createTestProject(t, store, "owner", "repo", true, 60)
	client := &fakeGitHubClient{snapshots: map[string]RepositorySnapshot{
		"owner/repo": snapshotAt(now, 10, 4, "head-one"),
	}, errors: map[string]error{}}
	service := NewService(store, client, time.Second, 2)

	for range 2 {
		if _, err := service.SyncProject(context.Background(), project.ID); err != nil {
			t.Fatalf("SyncProject() error = %v", err)
		}
	}
	client.mu.Lock()
	client.snapshots["owner/repo"] = snapshotAt(now.Add(time.Minute), 10, 4, "head-two")
	client.mu.Unlock()
	if _, err := service.SyncProject(context.Background(), project.ID); err != nil {
		t.Fatalf("SyncProject(changed head) error = %v", err)
	}

	snapshot, err := store.ProjectSnapshot(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("ProjectSnapshot() error = %v", err)
	}
	if len(snapshot.Issues) != 1 || len(snapshot.Issues[0].Comments) != 1 || len(snapshot.Issues[0].PullRequests) != 1 {
		t.Fatalf("snapshot contains duplicates: %#v", snapshot)
	}
	pullRequest := snapshot.Issues[0].PullRequests[0]
	if pullRequest.HeadSHA != "head-two" || pullRequest.CI.HeadSHA != "head-two" {
		t.Fatalf("head was not replaced: %#v", pullRequest)
	}
}

func TestSyncProjectsIsolatesRepositoryFailure(t *testing.T) {
	store := openTestStore(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	failed := createTestProject(t, store, "bad", "repo", true, 60)
	healthy := createTestProject(t, store, "good", "repo", true, 60)
	client := &fakeGitHubClient{
		snapshots: map[string]RepositorySnapshot{"good/repo": snapshotAt(now, 20, 5, "healthy-head")},
		errors:    map[string]error{"bad/repo": errors.New("upstream unavailable")},
	}
	service := NewService(store, client, time.Second, 2)

	results := service.SyncProjects(context.Background(), []int64{failed.ID, healthy.ID})
	if len(results) != 2 || results[0].Error == nil || results[1].Error != nil {
		t.Fatalf("results = %#v", results)
	}
	failedProject, _ := store.GetProject(context.Background(), failed.ID)
	healthyProject, _ := store.GetProject(context.Background(), healthy.ID)
	if failedProject.SyncStatus != "failed" || healthyProject.SyncStatus != "healthy" {
		t.Fatalf("statuses = %q, %q", failedProject.SyncStatus, healthyProject.SyncStatus)
	}
	if _, err := store.ProjectSnapshot(context.Background(), healthy.ID); err != nil {
		t.Fatalf("healthy snapshot unavailable: %v", err)
	}
}

func TestPollerHonorsPerProjectInterval(t *testing.T) {
	store := openTestStore(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	project := createTestProject(t, store, "owner", "repo", true, 60)
	client := &fakeGitHubClient{
		snapshots: map[string]RepositorySnapshot{"owner/repo": snapshotAt(now, 30, 6, "head")},
		errors:    map[string]error{},
	}
	service := NewService(store, client, time.Second, 1)
	poller := NewPoller(store, service, time.Second)
	poller.now = func() time.Time { return now }

	if results := poller.tick(context.Background()); len(results) != 1 || results[0].Error != nil {
		t.Fatalf("first tick = %#v", results)
	}
	if results := poller.tick(context.Background()); len(results) != 0 {
		t.Fatalf("second tick should be deferred, got %#v", results)
	}
	client.mu.Lock()
	calls := client.calls["owner/repo"]
	client.mu.Unlock()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if project.PollIntervalSeconds != 60 {
		t.Fatalf("poll interval = %d", project.PollIntervalSeconds)
	}
}

func TestProjectAndSnapshotPersistAcrossDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cddm.db")
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("database.Open(first) error = %v", err)
	}
	store := NewStore(db)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	project := createTestProject(t, store, "persisted", "repo", true, 60)
	if err := store.ReplaceSnapshot(context.Background(), project.ID, snapshotAt(now, 44, 8, "persisted-head")); err != nil {
		t.Fatalf("ReplaceSnapshot() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	reopened, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("database.Open(second) error = %v", err)
	}
	defer reopened.Close()
	snapshot, err := NewStore(reopened).ProjectSnapshot(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("ProjectSnapshot() after reopen error = %v", err)
	}
	if snapshot.Project.Owner != "persisted" || len(snapshot.Issues) != 1 || snapshot.Issues[0].PullRequests[0].HeadSHA != "persisted-head" {
		t.Fatalf("persisted snapshot = %#v", snapshot)
	}
}

func TestPollerRetriesStaleInterruptedSync(t *testing.T) {
	store := openTestStore(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now.Add(-2 * time.Minute) }
	project := createTestProject(t, store, "owner", "interrupted", true, 3600)
	if err := store.MarkSyncStarted(context.Background(), project.ID); err != nil {
		t.Fatalf("MarkSyncStarted() error = %v", err)
	}
	store.now = func() time.Time { return now }
	client := &fakeGitHubClient{
		snapshots: map[string]RepositorySnapshot{"owner/interrupted": snapshotAt(now, 50, 9, "recovered-head")},
		errors:    map[string]error{},
	}
	service := NewService(store, client, time.Minute, 1)
	poller := NewPoller(store, service, time.Second)
	poller.now = func() time.Time { return now }

	results := poller.tick(context.Background())
	if len(results) != 1 || results[0].Error != nil {
		t.Fatalf("stale sync tick = %#v", results)
	}
	recovered, err := store.GetProject(context.Background(), project.ID)
	if err != nil || recovered.SyncStatus != "healthy" {
		t.Fatalf("recovered project = %#v, error = %v", recovered, err)
	}
}
