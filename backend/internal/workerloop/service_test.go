package workerloop

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

const (
	headA = "241401d9f5c1fb2004eeb19ec612323f74b57199"
	headB = "341401d9f5c1fb2004eeb19ec612323f74b57199"
)

func TestCandidateResultCompletesCommand(t *testing.T) {
	db, project, store, service := testService(t, ":memory:")
	_ = db
	command := createCommand(t, store, project.ID, "cmd-candidate", "implementor", "", CommandAwaitingResult)
	snapshot := projectSnapshot(project, issueWithComments(140, comment(1001, candidateMarker(command.ID, 150, headA))))
	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandCompleted)
	results := commandResults(t, store, project.ID, command.ID)
	if len(results) != 1 || results[0].ValidationStatus != ValidationAccepted {
		t.Fatalf("results = %+v", results)
	}
}

func TestUnknownAndWrongRoleResultsRemainEvidenceOnly(t *testing.T) {
	_, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-qa", "qa", headA, CommandAwaitingResult)
	snapshot := projectSnapshot(project, issueWithComments(140,
		comment(1001, continueMarker("cmd-unknown")),
		comment(1002, continueMarker(command.ID)),
	))
	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAwaitingResult)
	unknown := commandResults(t, store, project.ID, "cmd-unknown")
	if len(unknown) != 1 || unknown[0].ValidationStatus != ValidationUnbound {
		t.Fatalf("unknown = %+v", unknown)
	}
	wrong := commandResults(t, store, project.ID, command.ID)
	if len(wrong) != 1 || wrong[0].ValidationStatus != ValidationWrongRole {
		t.Fatalf("wrong = %+v", wrong)
	}
}

func TestStaleQAResultDoesNotCompleteCommand(t *testing.T) {
	_, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-qa", "qa", headA, CommandAwaitingResult)
	body := marker(fmt.Sprintf(`{"version":1,"role":"qa","result":"approved","command_id":%q,"reviewed_head":%q,"blocking_findings":0}`, command.ID, headB))
	if err := service.ObserveProjectSnapshot(context.Background(), projectSnapshot(project, issueWithComments(140, comment(1001, body)))); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAwaitingResult)
	results := commandResults(t, store, project.ID, command.ID)
	if len(results) != 1 || results[0].ValidationStatus != ValidationStale {
		t.Fatalf("results = %+v", results)
	}
}

func TestConflictingTerminalResultsBecomeAmbiguous(t *testing.T) {
	_, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-conflict", "implementor", "", CommandAwaitingResult)
	snapshot := projectSnapshot(project, issueWithComments(140,
		comment(1001, candidateMarker(command.ID, 150, headA)),
		comment(1002, candidateMarker(command.ID, 150, headB)),
	))
	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAmbiguous)
	for _, result := range commandResults(t, store, project.ID, command.ID) {
		if result.ValidationStatus != ValidationAmbiguous {
			t.Fatalf("result = %+v", result)
		}
	}
}

func TestDuplicateSyncIsIdempotent(t *testing.T) {
	db, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-repeat", "implementor", "", CommandAwaitingResult)
	snapshot := projectSnapshot(project, issueWithComments(140, comment(1001, continueMarker(command.ID))))
	for range 2 {
		if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	assertCommandStatus(t, store, command.ID, CommandCompleted)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM workflow_results WHERE project_id=? AND github_comment_id=?`, project.ID, 1001).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestRemovedAcceptedMarkerMakesTerminalCommandAmbiguous(t *testing.T) {
	_, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-removed", "implementor", "", CommandAwaitingResult)
	withMarker := projectSnapshot(project, issueWithComments(140, comment(1001, continueMarker(command.ID))))
	if err := service.ObserveProjectSnapshot(context.Background(), withMarker); err != nil {
		t.Fatal(err)
	}
	if err := service.ObserveProjectSnapshot(context.Background(), projectSnapshot(project, issueWithComments(140))); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAmbiguous)
}

func TestCommandAndResultSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker-loop.db")
	db, project, store, service := testService(t, path)
	command := createCommand(t, store, project.ID, "cmd-restart", "implementor", "", CommandAwaitingResult)
	if err := service.ObserveProjectSnapshot(context.Background(), projectSnapshot(project, issueWithComments(140, comment(1001, continueMarker(command.ID))))); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := NewStore(reopened)
	assertCommandStatus(t, restarted, command.ID, CommandCompleted)
	if results := commandResults(t, restarted, project.ID, command.ID); len(results) != 1 || results[0].GitHubCommentID != 1001 {
		t.Fatalf("results = %+v", results)
	}
}

func testService(t *testing.T, path string) (*sql.DB, supervisor.Project, *Store, *Service) {
	t.Helper()
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if path == ":memory:" {
		t.Cleanup(func() { db.Close() })
	}
	project, err := supervisor.NewStore(db).CreateProject(context.Background(), supervisor.CreateProjectInput{
		Owner: "NordCoder", Repository: "misak-website", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	return db, project, store, NewService(store)
}

func createCommand(t *testing.T, store *Store, projectID int64, id, role, expectedHead, status string) Command {
	t.Helper()
	command, err := store.CreateCommand(context.Background(), CreateCommandInput{
		ID: id, ProjectID: projectID, IssueNumber: 140, IdentityKey: id + "-identity", Role: role,
		Action: "dispatch", ResourceProfile: "cddm-dashboard-resources/v1.0", ContextHash: "context-hash", ExpectedHead: expectedHead, Status: status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func assertCommandStatus(t *testing.T, store *Store, id, want string) {
	t.Helper()
	command, err := store.GetCommand(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if command.Status != want {
		t.Fatalf("status=%s want=%s", command.Status, want)
	}
}

func commandResults(t *testing.T, store *Store, projectID int64, commandID string) []Result {
	t.Helper()
	results, err := store.ResultsForCommand(context.Background(), projectID, commandID)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

func projectSnapshot(project supervisor.Project, issues ...supervisor.Issue) supervisor.ProjectSnapshot {
	return supervisor.ProjectSnapshot{Project: project, Issues: issues}
}

func issueWithComments(number int, comments ...supervisor.Comment) supervisor.Issue {
	return supervisor.Issue{GitHubID: int64(number * 10), Number: number, State: "open", Comments: comments}
}

func comment(id int64, body string) supervisor.Comment {
	now := time.Unix(id, 0).UTC()
	return supervisor.Comment{GitHubID: id, Body: body, Author: "worker", CreatedAt: now, UpdatedAt: now}
}

func marker(payload string) string {
	return "<!-- cddm-dashboard:result\n" + payload + "\n-->"
}

func continueMarker(commandID string) string {
	return marker(fmt.Sprintf(`{"version":1,"role":"implementor","result":"continue","command_id":%q}`, commandID))
}

func candidateMarker(commandID string, pr int, head string) string {
	return marker(fmt.Sprintf(`{"version":1,"role":"implementor","result":"candidate_ready","command_id":%q,"pr":%d,"head":%q}`, commandID, pr, head))
}
