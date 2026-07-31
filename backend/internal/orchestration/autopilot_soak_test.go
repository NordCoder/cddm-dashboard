package orchestration_test

import (
	"context"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func TestContinuousAutopilotThreeIssueSoakHonorsWIPRolesAndDuplicateClaims(t *testing.T) {
	ctx := context.Background()
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 2, 1, 1)
	implementFirst := schedulerIntent(project.ID, commandID, "soak-impl-101", orchestration.ActionDispatch, 101, "implementor", 10, "project:1:issue:101:implementor")
	implementThird := schedulerIntent(project.ID, commandID, "soak-impl-103", orchestration.ActionDispatch, 103, "implementor", 20, "project:1:issue:103:implementor")
	qaSecond := schedulerIntent(project.ID, commandID, "soak-qa-102", orchestration.ActionDispatch, 102, "qa", 30, "project:1:issue:102:qa:head")
	qaSecond.ExpectedHead = otherHead
	inputs := []orchestration.IntentInput{implementFirst, implementThird, qaSecond}
	createSchedulerIntents(t, store, project.ID, commandID, inputs)
	if _, err := store.CreateBatch(ctx, nil, append([]orchestration.IntentInput(nil), inputs...)); err != nil {
		t.Fatalf("duplicate Lead action batch: %v", err)
	}

	first := claim(t, scheduler, project.ID, "three-issue-first", snapshot)
	if first.Intent.IssueNumber != 101 || first.Intent.Role != "implementor" {
		t.Fatalf("first three-Issue claim = %+v", first.Intent)
	}
	duplicate, err := scheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "three-issue-first", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot,
	})
	if err != nil || !duplicate.Claimed || duplicate.Lease == nil || duplicate.Lease.ID != first.Lease.ID {
		t.Fatalf("duplicate claim = %+v err=%v", duplicate, err)
	}
	second := claim(t, scheduler, project.ID, "three-issue-second", snapshot)
	if second.Intent.IssueNumber != 102 || second.Intent.Role != "qa" {
		t.Fatalf("role-limited second claim = %+v", second.Intent)
	}
	blocked, err := scheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "three-issue-blocked", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot,
	})
	if err != nil || blocked.Claimed || blocked.Reason != "no_runnable_intent" {
		t.Fatalf("three-Issue WIP saturation = %+v err=%v", blocked, err)
	}
	if _, err := scheduler.Transition(ctx, orchestration.LeaseTransition{
		ProjectID: project.ID, LeaseID: first.Lease.ID, LeaseOwner: first.Lease.LeaseOwner,
		LeaseToken: first.Lease.LeaseToken, Target: orchestration.LeaseCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	third := claim(t, scheduler, project.ID, "three-issue-third", snapshot)
	if third.Intent.IssueNumber != 103 || third.Intent.Role != "implementor" {
		t.Fatalf("third Issue did not enter after WIP release: %+v", third.Intent)
	}
}

func TestPauseStopAndBreakerPreserveAmbiguousEvidence(t *testing.T) {
	ctx := context.Background()
	db, project, store, _, _, commandID := schedulerFixture(t, ":memory:", 2, 2, 2)
	input := schedulerIntent(project.ID, commandID, "ambiguous-soak", orchestration.ActionDispatch, 101, "implementor", 10, "project:1:issue:101:implementor")
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{input})
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `UPDATE workflow_intents SET status='ambiguous',updated_at=? WHERE id=?`, now.Format(time.RFC3339Nano), input.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,status,reason_code,created_at,updated_at
	) VALUES(?,?,?,?,?,?,'ambiguous','conflicting_terminal_results',?,?)`,
		"materialization-ambiguous-soak", project.ID, input.ID, "lease-ambiguous-soak", "provision-ambiguous-soak",
		input.LaneKey, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	results := workerloop.NewStore(db)
	result := workerloop.Result{
		ProjectID: project.ID, GitHubCommentID: 99001, IssueNumber: 101, CommandID: commandID,
		Role: "implementor", Result: "candidate_ready", Payload: []byte(`{"version":2}`), PayloadHash: "soak-result",
		ValidationStatus: workerloop.ValidationAmbiguous, ValidationReason: "conflicting_terminal_results", ObservedAt: now,
	}
	if err := results.UpsertResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err := results.UpsertResult(ctx, result); err != nil {
		t.Fatalf("duplicate result/comment replay: %v", err)
	}

	operations := orchestration.NewOperationsService(store)
	paused, err := operations.Pause(ctx, project.ID, 0)
	if err != nil || paused.Profile.AutonomyState != orchestration.AutonomyStatePaused {
		t.Fatalf("pause = %+v err=%v", paused, err)
	}
	tripped, err := operations.TripBreaker(ctx, orchestration.BreakerTripInput{
		ProjectID: project.ID, ExpectedRevision: 1, Code: orchestration.BreakerAmbiguousWorkerResult,
		Reason: "conflicting terminal worker result evidence",
	})
	if err != nil || tripped.Counts.ActiveCircuitBreakers != 1 {
		t.Fatalf("trip = %+v err=%v", tripped, err)
	}
	stopped, err := operations.Stop(ctx, project.ID, 2)
	if err != nil || stopped.Profile.AutonomyState != orchestration.AutonomyStateStopped || stopped.Counts.AmbiguousRecords < 3 {
		t.Fatalf("stop projection = %+v err=%v", stopped, err)
	}
	stored, err := store.Intent(ctx, input.ID)
	if err != nil || stored.Status != orchestration.IntentAmbiguous {
		t.Fatalf("ambiguous Intent after stop = %+v err=%v", stored, err)
	}
	var materializationStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM autonomous_command_materializations WHERE intent_id=?`, input.ID).Scan(&materializationStatus); err != nil || materializationStatus != orchestration.AutonomousMaterializationAmbiguous {
		t.Fatalf("ambiguous materialization after stop = %s err=%v", materializationStatus, err)
	}
	storedResults, err := results.ResultsForCommand(ctx, project.ID, commandID)
	ambiguousResults := 0
	for _, storedResult := range storedResults {
		if storedResult.GitHubCommentID == result.GitHubCommentID && storedResult.ValidationStatus == workerloop.ValidationAmbiguous {
			ambiguousResults++
		}
	}
	if err != nil || len(storedResults) != 2 || ambiguousResults != 1 {
		t.Fatalf("ambiguous result after duplicate/stop = %+v err=%v", storedResults, err)
	}
}

func soakIntent(projectID int64, commandID, suffix string, issue int, role, lane, wave string, priority int) orchestration.IntentInput {
	return orchestration.IntentInput{
		ID: "intent-soak-" + suffix, ProjectID: projectID, SourceResultCommentID: 2001,
		SourceCommandID: commandID, ActionID: "soak-" + suffix, ActionType: orchestration.ActionDispatch,
		Repository: "NordCoder/app", IssueNumber: issue, Role: role, WaveID: wave,
		Priority: priority, LaneKey: lane, Status: orchestration.IntentPending,
	}
}
