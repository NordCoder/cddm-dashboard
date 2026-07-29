package orchestration_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func TestSchedulerUsesDeterministicPriorityOrder(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 4, 3, 3)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "dispatch", orchestration.ActionDispatch, 103, "implementor", 50, "project:1:issue:103:implementor"),
		schedulerIntent(project.ID, commandID, "merge", orchestration.ActionMerge, 102, "lead", 20, "project:1:lead"),
		schedulerIntent(project.ID, commandID, "correct", orchestration.ActionCorrect, 101, "implementor", 10, "project:1:issue:101:implementor"),
		schedulerIntent(project.ID, commandID, "plan", orchestration.ActionPlanNextWave, 90, "lead", 100, "project:1:lead"),
	})

	first := claim(t, scheduler, project.ID, "claim-1", snapshot)
	if first.Intent.ActionID != "correct" {
		t.Fatalf("first intent = %+v", first.Intent)
	}
	second := claim(t, scheduler, project.ID, "claim-2", snapshot)
	if second.Intent.ActionID != "merge" {
		t.Fatalf("second intent = %+v", second.Intent)
	}
	third := claim(t, scheduler, project.ID, "claim-3", snapshot)
	if third.Intent.ActionID != "dispatch" {
		t.Fatalf("third intent = %+v", third.Intent)
	}
	blocked, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{ProjectID: project.ID, ClaimID: "claim-4", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot})
	if err != nil || blocked.Claimed || blocked.Reason != "no_runnable_intent" {
		t.Fatalf("Lead lane serialization decision = %+v err=%v", blocked, err)
	}
	if _, err := scheduler.Transition(context.Background(), orchestration.LeaseTransition{ProjectID: project.ID, LeaseID: second.Lease.ID, LeaseOwner: "scheduler", LeaseToken: second.Lease.LeaseToken, Target: orchestration.LeaseCompleted}); err != nil {
		t.Fatal(err)
	}
	plan := claim(t, scheduler, project.ID, "claim-5", snapshot)
	if plan.Intent.ActionID != "plan" {
		t.Fatalf("plan intent = %+v", plan.Intent)
	}
}

func TestSchedulerEnforcesWIPAndIssueLaneUntilRelease(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 1, 1, 1)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "issue-101", orchestration.ActionDispatch, 101, "implementor", 50, "project:1:issue:101:implementor"),
		schedulerIntent(project.ID, commandID, "issue-102", orchestration.ActionDispatch, 102, "implementor", 50, "project:1:issue:102:implementor"),
	})
	first := claim(t, scheduler, project.ID, "wip-1", snapshot)
	decision, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{ProjectID: project.ID, ClaimID: "wip-2", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot})
	if err != nil || decision.Claimed {
		t.Fatalf("WIP saturation decision = %+v err=%v", decision, err)
	}
	if _, err := scheduler.Transition(context.Background(), orchestration.LeaseTransition{ProjectID: project.ID, LeaseID: first.Lease.ID, LeaseOwner: "wrong", LeaseToken: first.Lease.LeaseToken, Target: orchestration.LeaseReleased}); !errors.Is(err, orchestration.ErrConflict) {
		t.Fatalf("wrong owner transition error = %v", err)
	}
	if _, err := scheduler.Transition(context.Background(), orchestration.LeaseTransition{ProjectID: project.ID, LeaseID: first.Lease.ID, LeaseOwner: "scheduler", LeaseToken: first.Lease.LeaseToken, Target: orchestration.LeaseReleased}); err != nil {
		t.Fatal(err)
	}
	second := claim(t, scheduler, project.ID, "wip-3", snapshot)
	if second.Intent.IssueNumber != 101 {
		t.Fatalf("released highest-priority Intent was not reclaimed: %+v", second.Intent)
	}
}

func TestSchedulerRespectsIssueAndProjectBlockingScopes(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 3, 3, 3)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		blockedIntent(project.ID, commandID, "hold-101", 101),
		schedulerIntent(project.ID, commandID, "blocked-work", orchestration.ActionDispatch, 101, "implementor", 10, "project:1:issue:101:implementor"),
		schedulerIntent(project.ID, commandID, "free-work", orchestration.ActionDispatch, 102, "implementor", 50, "project:1:issue:102:implementor"),
	})
	decision := claim(t, scheduler, project.ID, "scope-1", snapshot)
	if decision.Intent.ActionID != "free-work" {
		t.Fatalf("Issue-scoped hold did not isolate scope: %+v", decision.Intent)
	}
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{blockedIntent(project.ID, commandID, "global-hold", 0)})
	blocked, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{ProjectID: project.ID, ClaimID: "scope-2", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot})
	if err != nil || blocked.Claimed || blocked.Reason != "project_blocked" {
		t.Fatalf("project block decision = %+v err=%v", blocked, err)
	}
}

func TestSchedulerDoesNotClaimPausedProject(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 3, 3, 3)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "paused-work", orchestration.ActionDispatch, 101, "implementor", 50, "project:1:issue:101:implementor"),
	})
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous, AutonomyState: orchestration.AutonomyStatePaused, ControlIssueNumber: 90}); err != nil {
		t.Fatal(err)
	}
	decision, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{ProjectID: project.ID, ClaimID: "paused-1", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot})
	if err != nil || decision.Claimed || decision.Reason != "autonomy_not_enabled" {
		t.Fatalf("paused decision = %+v err=%v", decision, err)
	}
}

func TestSchedulerSupersedesStaleCandidateAndClaimsNext(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 3, 3, 3)
	stale := schedulerIntent(project.ID, commandID, "stale-merge", orchestration.ActionMerge, 101, "lead", 10, "project:1:lead")
	stale.PRNumber = 150
	stale.ExpectedHead = otherHead
	valid := schedulerIntent(project.ID, commandID, "valid-work", orchestration.ActionDispatch, 102, "implementor", 50, "project:1:issue:102:implementor")
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{stale, valid})
	decision := claim(t, scheduler, project.ID, "stale-1", snapshot)
	if decision.Intent.ActionID != "valid-work" {
		t.Fatalf("stale Intent was retargeted or selected: %+v", decision.Intent)
	}
	stored, err := store.Intent(context.Background(), stale.ID)
	if err != nil || stored.Status != orchestration.IntentSuperseded {
		t.Fatalf("stale status = %+v err=%v", stored, err)
	}
}

func TestSchedulerLeaseExpiryAndClaimIdempotencySurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduler.db")
	db, project, store, scheduler, snapshot, commandID := schedulerFixture(t, path, 1, 1, 1)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "expiring", orchestration.ActionDispatch, 101, "implementor", 50, "project:1:issue:101:implementor"),
	})
	first := claimWithTTL(t, scheduler, project.ID, "expiry-1", snapshot, 5*time.Millisecond)
	again, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{ProjectID: project.ID, ClaimID: "expiry-1", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot})
	if err != nil || !again.Claimed || again.Lease.ID != first.Lease.ID {
		t.Fatalf("idempotent claim = %+v err=%v", again, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	reopened, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := orchestration.NewScheduler(orchestration.NewStore(reopened))
	second := claim(t, restarted, project.ID, "expiry-2", snapshot)
	if second.Intent.ID != first.Intent.ID || second.Lease.ID == first.Lease.ID {
		t.Fatalf("expired Intent was not safely reclaimed: first=%+v second=%+v", first, second)
	}
	old, err := restarted.Lease(context.Background(), project.ID, first.Lease.ID)
	if err != nil || old.Status != orchestration.LeaseExpired {
		t.Fatalf("old lease = %+v err=%v", old, err)
	}
}

func TestConcurrentClaimsCannotSplitOneIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, path, 3, 3, 3)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "single", orchestration.ActionDispatch, 101, "implementor", 50, "project:1:issue:101:implementor"),
	})
	start := make(chan struct{})
	results := make(chan orchestration.ClaimDecision, 2)
	errorsChannel := make(chan error, 2)
	var group sync.WaitGroup
	for _, claimID := range []string{"concurrent-1", "concurrent-2"} {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			<-start
			decision, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{ProjectID: project.ID, ClaimID: id, LeaseOwner: id, LeaseTTL: time.Minute, Snapshot: snapshot})
			results <- decision
			errorsChannel <- err
		}(claimID)
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent claim error = %v", err)
		}
	}
	claimed := 0
	for result := range results {
		if result.Claimed {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed count = %d, want 1", claimed)
	}
	leases, err := scheduler.ListLeases(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, lease := range leases {
		if lease.Status == orchestration.LeaseActive {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active leases = %d, leases=%+v", active, leases)
	}
}

func schedulerFixture(t *testing.T, path string, maxWork, maxImplementors, maxQA int) (*sql.DB, supervisor.Project, *orchestration.Store, *orchestration.Scheduler, supervisor.ProjectSnapshot, string) {
	t.Helper()
	db, project := testProject(t, path)
	store := orchestration.NewStore(db)
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		MaxActiveWorkUnits: maxWork, MaxParallelImplementors: maxImplementors, MaxParallelQA: maxQA,
	}); err != nil {
		t.Fatal(err)
	}
	command, _ := seedLeadResult(t, db, project.ID)
	snapshot := supervisor.ProjectSnapshot{Project: project, Issues: []supervisor.Issue{
		{Number: 90, State: "open"},
		{Number: 101, State: "open", PullRequests: []supervisor.PullRequest{{Number: 150, HeadSHA: testHead, State: "open"}}},
		{Number: 102, State: "open", PullRequests: []supervisor.PullRequest{{Number: 151, HeadSHA: otherHead, State: "open"}}},
		{Number: 103, State: "open"},
	}}
	return db, project, store, orchestration.NewScheduler(store), snapshot, command.ID
}

func createSchedulerIntents(t *testing.T, store *orchestration.Store, projectID int64, commandID string, intents []orchestration.IntentInput) {
	t.Helper()
	if _, err := store.CreateBatch(context.Background(), nil, intents); err != nil {
		t.Fatal(err)
	}
}

func schedulerIntent(projectID int64, commandID, actionID, actionType string, issue int, role string, priority int, lane string) orchestration.IntentInput {
	value := orchestration.IntentInput{
		ID: "intent-" + actionID, ProjectID: projectID, SourceResultCommentID: 2001,
		SourceCommandID: commandID, ActionID: actionID, ActionType: actionType,
		Repository: "NordCoder/app", IssueNumber: issue, Role: role,
		Priority: priority, LaneKey: lane, Status: orchestration.IntentPending,
	}
	switch actionType {
	case orchestration.ActionCorrect:
		value.ExpectedPreviousHead = testHead
	case orchestration.ActionMerge:
		value.PRNumber = 151
		value.ExpectedHead = otherHead
	case orchestration.ActionDispatch:
		if role == "qa" {
			value.ExpectedHead = testHead
		}
	}
	return value
}

func blockedIntent(projectID int64, commandID, actionID string, issue int) orchestration.IntentInput {
	return orchestration.IntentInput{
		ID: "intent-" + actionID, ProjectID: projectID, SourceResultCommentID: 2001,
		SourceCommandID: commandID, ActionID: actionID, ActionType: orchestration.ActionHold,
		Repository: "NordCoder/app", IssueNumber: issue, ReasonCode: "blocked",
		Priority: 5, Status: orchestration.IntentBlocked,
	}
}

func claim(t *testing.T, scheduler *orchestration.Scheduler, projectID int64, claimID string, snapshot supervisor.ProjectSnapshot) orchestration.ClaimDecision {
	t.Helper()
	return claimWithTTL(t, scheduler, projectID, claimID, snapshot, time.Minute)
}

func claimWithTTL(t *testing.T, scheduler *orchestration.Scheduler, projectID int64, claimID string, snapshot supervisor.ProjectSnapshot, ttl time.Duration) orchestration.ClaimDecision {
	t.Helper()
	decision, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{ProjectID: projectID, ClaimID: claimID, LeaseOwner: "scheduler", LeaseTTL: ttl, Snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Claimed || decision.Intent == nil || decision.Lease == nil {
		t.Fatalf("claim %s = %+v", claimID, decision)
	}
	return decision
}
