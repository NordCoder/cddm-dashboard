package orchestration_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func TestContinuousAutopilotRestartProjectionPreservesEveryDurableStage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "autopilot-soak.db")
	db, project, store, _, _, sourceCommandID := schedulerFixture(t, path, 5, 5, 5)
	wave := &orchestration.WaveInput{
		ProjectID: project.ID, WaveID: "wave-restart-soak", ControlIssueNumber: 90,
		SourceCommandID: sourceCommandID, Status: orchestration.WaveActive, Issues: []int{101, 102, 103},
	}
	inputs := []orchestration.IntentInput{
		soakIntent(project.ID, sourceCommandID, "pending", 101, "implementor", "project:1:issue:101:implementor", wave.WaveID, 10),
		soakIntent(project.ID, sourceCommandID, "claimed", 102, "implementor", "project:1:issue:102:implementor", wave.WaveID, 20),
		soakIntent(project.ID, sourceCommandID, "provisioned", 103, "qa", "project:1:issue:103:qa:head", wave.WaveID, 30),
		soakIntent(project.ID, sourceCommandID, "delivery", 101, "qa", "project:1:issue:101:qa:head", wave.WaveID, 40),
		soakIntent(project.ID, sourceCommandID, "awaiting", 90, "lead", "project:1:lead", wave.WaveID, 50),
	}
	created, err := store.CreateBatch(ctx, wave, append([]orchestration.IntentInput(nil), inputs...))
	if err != nil || len(created) != len(inputs) {
		t.Fatalf("create restart fixture = %+v err=%v", created, err)
	}
	replayed, err := store.CreateBatch(ctx, wave, append([]orchestration.IntentInput(nil), inputs...))
	if err != nil || len(replayed) != len(inputs) {
		t.Fatalf("idempotent action replay = %+v err=%v", replayed, err)
	}

	now := time.Now().UTC()
	for index := 1; index < len(inputs); index++ {
		if _, err := db.ExecContext(ctx, `UPDATE workflow_intents SET status='claimed',updated_at=? WHERE id=?`, now.Format(time.RFC3339Nano), inputs[index].ID); err != nil {
			t.Fatal(err)
		}
		insertSoakLease(t, db, project.ID, inputs[index], now)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO session_provision_requests(
		id,project_id,intent_id,lane_lease_id,lane_key,issue_number,role,expected_head,attachment_profile,
		attachments_json,bootstrap_text,session_policy,chatgpt_project_url,status,worker_id,tab_id,target_kind,
		target_origin,target_path,attachment_evidence_json,observed_chatgpt_url,bound_binding_id,bound_binding_version,
		created_at,updated_at,completed_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'provisioned',?,?,?,?,?,?,?,?,?,?,?,?)`,
		"provision-soak", project.ID, inputs[2].ID, soakLeaseID(inputs[2].ID), inputs[2].LaneKey, inputs[2].IssueNumber,
		inputs[2].Role, testHead, "cddm-dashboard-attachments/v2:qa:bootstrap", `["03-qa-trigger.md"]`,
		"Wait for a Workflow Command.", "fresh_per_intent", finalProjectURL, "worker-soak", 77,
		"chatgpt_conversation", "https://chatgpt.com", "/c/soak", `["03-qa-trigger.md"]`,
		"https://chatgpt.com/g/g-project/repository/c/soak", "binding-soak", 1,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	commands := workerloop.NewStore(db)
	commandFixtures := []struct {
		id       string
		identity string
		status   string
		intent   orchestration.IntentInput
	}{
		{id: "cmd-soak-delivery", identity: "soak-delivery", status: workerloop.CommandDeliveryPending, intent: inputs[3]},
		{id: "cmd-soak-awaiting", identity: "soak-awaiting", status: workerloop.CommandAwaitingResult, intent: inputs[4]},
	}
	for _, fixture := range commandFixtures {
		if _, err := commands.CreateCommand(ctx, workerloop.CreateCommandInput{
			ID: fixture.id, ProjectID: project.ID, IssueNumber: fixture.intent.IssueNumber, IdentityKey: fixture.identity,
			Role: fixture.intent.Role, Action: "dispatch", ResourceProfile: orchestration.ContinuousResourceProfile,
			ContextHash: fixture.id + "-context", ExpectedHead: testHead, Status: fixture.status,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO autonomous_command_materializations(
			id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,status,workflow_command_id,
			context_hash,prompt_hash,created_at,updated_at
		) VALUES(?,?,?,?,?,?,'materialized',?,?,?,?,?)`,
			"materialization-"+fixture.id, project.ID, fixture.intent.ID, soakLeaseID(fixture.intent.ID), "provision-"+fixture.id,
			fixture.intent.LaneKey, fixture.id, fixture.id+"-context", fixture.id+"-prompt",
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	status, err := orchestration.NewOperationsService(orchestration.NewStore(reopened)).Status(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.ActiveWave == nil || status.ActiveWave.WaveID != wave.WaveID || len(status.ActiveWave.Issues) != 3 {
		t.Fatalf("restart Wave projection = %+v", status.ActiveWave)
	}
	if status.Counts.PendingIntents != 1 || status.Counts.ClaimedIntents != 4 || status.Counts.ActiveLeases != 4 {
		t.Fatalf("restart queue counts = %+v", status.Counts)
	}
	if status.Counts.ManagedSessions != 1 || status.Counts.ActiveCommands != 2 || len(status.Queue) != 5 {
		t.Fatalf("restart execution projection = counts=%+v queue=%d", status.Counts, len(status.Queue))
	}
	seenCommands := map[string]bool{}
	for _, command := range status.Commands {
		seenCommands[command.WorkflowCommandID] = true
	}
	if !seenCommands["cmd-soak-delivery"] || !seenCommands["cmd-soak-awaiting"] {
		t.Fatalf("restart command identities = %+v", seenCommands)
	}
}

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
	if err != nil || len(storedResults) != 1 || storedResults[0].ValidationStatus != workerloop.ValidationAmbiguous {
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

func soakLeaseID(intentID string) string { return "lease-" + intentID }

func insertSoakLease(t *testing.T, db *sql.DB, projectID int64, input orchestration.IntentInput, now time.Time) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO workflow_lane_leases(
		id,project_id,lane_key,intent_id,claim_id,lease_owner,lease_token,status,acquired_at,expires_at
	) VALUES(?,?,?,?,?,?,?,'active',?,?)`,
		soakLeaseID(input.ID), projectID, input.LaneKey, input.ID, "claim-"+input.ID, "soak-scheduler",
		"token-"+input.ID, now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}
