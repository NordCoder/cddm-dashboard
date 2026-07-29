package orchestration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
)

func TestAutopilotControlLifecycleUsesOptimisticRevision(t *testing.T) {
	db, project := testProject(t, ":memory:")
	store := orchestration.NewStore(db)
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateDisabled, ControlIssueNumber: 90,
		ChatGPTProjectURL: "https://chatgpt.com/g/g-project/example/project",
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE projects SET sync_status='healthy',sync_error='',last_sync_completed_at=? WHERE id=?`, now, project.ID); err != nil {
		t.Fatal(err)
	}
	service := orchestration.NewOperationsService(store)
	initial, err := service.Status(context.Background(), project.ID)
	if err != nil || initial.Control.Revision != 0 {
		t.Fatalf("initial status=%+v err=%v", initial, err)
	}
	enabled, err := service.Enable(context.Background(), project.ID, 0)
	if err != nil || enabled.Profile.AutonomyState != orchestration.AutonomyStateEnabled || enabled.Control.Revision != 1 {
		t.Fatalf("enabled=%+v err=%v", enabled, err)
	}
	replayed, err := service.Enable(context.Background(), project.ID, 0)
	if err != nil || replayed.Control.Revision != 1 {
		t.Fatalf("idempotent enable=%+v err=%v", replayed, err)
	}
	if _, err := service.Pause(context.Background(), project.ID, 0); !errors.Is(err, orchestration.ErrConflict) {
		t.Fatalf("stale pause error=%v", err)
	}
	paused, err := service.Pause(context.Background(), project.ID, 1)
	if err != nil || paused.Profile.AutonomyState != orchestration.AutonomyStatePaused || paused.Control.Revision != 2 {
		t.Fatalf("paused=%+v err=%v", paused, err)
	}
	resumed, err := service.Resume(context.Background(), project.ID, 2)
	if err != nil || resumed.Profile.AutonomyState != orchestration.AutonomyStateEnabled || resumed.Control.Revision != 3 {
		t.Fatalf("resumed=%+v err=%v", resumed, err)
	}
	stopped, err := service.Stop(context.Background(), project.ID, 3)
	if err != nil || stopped.Profile.AutonomyState != orchestration.AutonomyStateStopped || stopped.Control.Revision != 4 {
		t.Fatalf("stopped=%+v err=%v", stopped, err)
	}
}

func TestAutopilotCircuitBreakersIsolateLaneAndProject(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 3, 3, 3)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "blocked-lane", orchestration.ActionDispatch, 101, "implementor", 10, "project:1:issue:101:implementor"),
		schedulerIntent(project.ID, commandID, "free-lane", orchestration.ActionDispatch, 102, "implementor", 20, "project:1:issue:102:implementor"),
	})
	service := orchestration.NewOperationsService(store)
	status, err := service.TripBreaker(context.Background(), orchestration.BreakerTripInput{
		ProjectID: project.ID, ExpectedRevision: 0, ScopeKind: orchestration.BreakerScopeLane,
		LaneKey: "project:1:issue:101:implementor", Code: orchestration.BreakerWorkerSessionConflict,
		Reason: "duplicate managed worker binding",
	})
	if err != nil || status.Control.Revision != 1 {
		t.Fatalf("trip lane status=%+v err=%v", status, err)
	}
	decision := claim(t, scheduler, project.ID, "breaker-lane", snapshot)
	if decision.Intent.ActionID != "free-lane" {
		t.Fatalf("lane breaker leaked across lanes: %+v", decision.Intent)
	}
	status, err = service.TripBreaker(context.Background(), orchestration.BreakerTripInput{
		ProjectID: project.ID, ExpectedRevision: 1, Code: orchestration.BreakerGitHubSynchronization,
		Reason: "synchronized repository facts are unhealthy",
	})
	if err != nil || status.Control.Revision != 2 {
		t.Fatalf("trip project status=%+v err=%v", status, err)
	}
	blocked, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "breaker-project", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot,
	})
	if err != nil || blocked.Claimed || blocked.Reason != "no_runnable_intent" {
		t.Fatalf("project breaker decision=%+v err=%v", blocked, err)
	}
	var projectBreakerID string
	for _, breaker := range status.CircuitBreakers {
		if breaker.ScopeKind == orchestration.BreakerScopeProject {
			projectBreakerID = breaker.ID
		}
	}
	acknowledged, err := service.AcknowledgeBreaker(context.Background(), orchestration.BreakerTransitionInput{ProjectID: project.ID, BreakerID: projectBreakerID, ExpectedRevision: 2})
	if err != nil || acknowledged.Control.Revision != 3 {
		t.Fatalf("acknowledge=%+v err=%v", acknowledged, err)
	}
	resolved, err := service.ResolveBreaker(context.Background(), orchestration.BreakerTransitionInput{ProjectID: project.ID, BreakerID: projectBreakerID, ExpectedRevision: 3})
	if err != nil || resolved.Control.Revision != 4 {
		t.Fatalf("resolve=%+v err=%v", resolved, err)
	}
}

func TestAutopilotStopPreservesClaimedWorkAndSupersedesOnlySafePending(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 3, 3, 3)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "active", orchestration.ActionDispatch, 101, "implementor", 10, "project:1:issue:101:implementor"),
		schedulerIntent(project.ID, commandID, "safe-pending", orchestration.ActionDispatch, 102, "implementor", 20, "project:1:issue:102:implementor"),
	})
	active := claim(t, scheduler, project.ID, "stop-active", snapshot)
	service := orchestration.NewOperationsService(store)
	stopped, err := service.Stop(context.Background(), project.ID, 0)
	if err != nil || stopped.Profile.AutonomyState != orchestration.AutonomyStateStopped {
		t.Fatalf("stop=%+v err=%v", stopped, err)
	}
	activeIntent, err := store.Intent(context.Background(), active.Intent.ID)
	if err != nil || activeIntent.Status != orchestration.IntentClaimed {
		t.Fatalf("active intent=%+v err=%v", activeIntent, err)
	}
	pendingIntent, err := store.Intent(context.Background(), "intent-safe-pending")
	if err != nil || pendingIntent.Status != orchestration.IntentSuperseded {
		t.Fatalf("pending intent=%+v err=%v", pendingIntent, err)
	}
	lease, err := scheduler.Lease(context.Background(), project.ID, active.Lease.ID)
	if err != nil || lease.Status != orchestration.LeaseActive {
		t.Fatalf("active lease=%+v err=%v", lease, err)
	}
}
