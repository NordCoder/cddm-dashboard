package orchestration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

const secondMergeCommit = "641401d9f5c1fb2004eeb19ec612323f74b57199"

func TestMultiIssueWaveSerializesLeadMergesAndPlansNextWaveOnce(t *testing.T) {
	ctx := context.Background()
	db, project := testProject(t, ":memory:")
	store := orchestration.NewStore(db)
	if _, err := store.UpdateProjectProfile(ctx, orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		DeliveryMode: "auto", MaxActiveWorkUnits: 3, MaxParallelImplementors: 2, MaxParallelQA: 2,
	}); err != nil {
		t.Fatal(err)
	}
	sourceCommand, sourceResultID := seedLeadResult(t, db, project.ID)
	lane := fmt.Sprintf("project:%d:lead", project.ID)
	created, err := store.CreateBatch(ctx, &orchestration.WaveInput{
		ProjectID: project.ID, WaveID: "wave-two", ControlIssueNumber: 90,
		SourceCommandID: sourceCommand.ID, Status: orchestration.WavePlanned, Issues: []int{101, 102},
	}, []orchestration.IntentInput{
		{
			ID: "intent-merge-first", ProjectID: project.ID, SourceResultCommentID: sourceResultID,
			SourceCommandID: sourceCommand.ID, ActionID: "merge-first", ActionType: orchestration.ActionMerge,
			Repository: "NordCoder/app", IssueNumber: 101, Role: "lead", PRNumber: 150,
			ExpectedHead: testHead, WaveID: "wave-two", Priority: 20, LaneKey: lane, Status: orchestration.IntentPending,
		},
		{
			ID: "intent-merge-second", ProjectID: project.ID, SourceResultCommentID: sourceResultID,
			SourceCommandID: sourceCommand.ID, ActionID: "merge-second", ActionType: orchestration.ActionMerge,
			Repository: "NordCoder/app", IssueNumber: 102, Role: "lead", PRNumber: 151,
			ExpectedHead: otherHead, WaveID: "wave-two", Priority: 21, LaneKey: lane, Status: orchestration.IntentPending,
		},
	})
	if err != nil || len(created) != 2 {
		t.Fatalf("create Wave = %+v err=%v", created, err)
	}
	snapshot := supervisor.ProjectSnapshot{Project: project, Issues: []supervisor.Issue{
		{Number: 90, State: "open"},
		{Number: 101, State: "open", PullRequests: []supervisor.PullRequest{{Number: 150, State: "open", BaseRef: "main", HeadSHA: testHead}}},
		{Number: 102, State: "open", PullRequests: []supervisor.PullRequest{{Number: 151, State: "open", BaseRef: "main", HeadSHA: otherHead}}},
	}}
	scheduler := orchestration.NewScheduler(store)
	first := claimAutopilotMerge(t, scheduler, project.ID, "wave-two-first", snapshot)
	blocked, err := scheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "wave-two-blocked", LeaseOwner: "dashboard-autopilot",
		LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || blocked.Claimed || blocked.Reason != "no_runnable_intent" {
		t.Fatalf("Lead lane was not serialized: %+v err=%v", blocked, err)
	}

	facts := &mergeFactsStub{}
	engine, err := orchestration.NewMergeAutopilotEngine(
		store, scheduler, provisioningService(t, store), mergePlannerStub{}, mergeDeliveryStub{},
		delivery.UnavailableBindingResolver{}, supervisor.NewStore(db), facts,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstCommand := seedMaterializedMergeCommand(t, db, project.ID, *first.Intent, *first.Lease, "cmd-wave-first", "merge-cycle-wave-first", "main")
	mergedAt := time.Now().UTC()
	facts.value = supervisor.MergeFacts{
		Repository: "NordCoder/app", IssueNumber: 101, IssueState: "closed", IssueLabels: []string{"status:done"},
		PRNumber: 150, PRState: "closed", Merged: true, ApprovedHead: testHead, BaseRef: "main",
		MergeCommit: mergeCommit, MergedAt: &mergedAt,
	}
	if err := engine.ReconcileCommandResult(ctx, firstCommand, acceptedMergeResult(project.ID, 101, 3101, firstCommand.ID, mergeCommit), mergedPayload(101, 150, firstCommand.ID, testHead, mergeCommit)); err != nil {
		t.Fatal(err)
	}
	assertNextWaveCount(t, store, project.ID, 0)
	wave, err := store.Wave(ctx, project.ID, "wave-two")
	if err != nil || wave.Status != orchestration.WaveActive {
		t.Fatalf("Wave after first merge = %+v err=%v", wave, err)
	}

	second := claimAutopilotMerge(t, scheduler, project.ID, "wave-two-second", snapshot)
	if second.Intent.IssueNumber != 102 {
		t.Fatalf("second merge Intent = %+v", second.Intent)
	}
	secondCommand := seedMaterializedMergeCommand(t, db, project.ID, *second.Intent, *second.Lease, "cmd-wave-second", "merge-cycle-wave-second", "main")
	facts.value = supervisor.MergeFacts{
		Repository: "NordCoder/app", IssueNumber: 102, IssueState: "closed", IssueLabels: []string{"status:done"},
		PRNumber: 151, PRState: "closed", Merged: true, ApprovedHead: otherHead, BaseRef: "main",
		MergeCommit: secondMergeCommit, MergedAt: &mergedAt,
	}
	if err := engine.ReconcileCommandResult(ctx, secondCommand, acceptedMergeResult(project.ID, 102, 3102, secondCommand.ID, secondMergeCommit), mergedPayload(102, 151, secondCommand.ID, otherHead, secondMergeCommit)); err != nil {
		t.Fatal(err)
	}
	assertNextWaveCount(t, store, project.ID, 1)
	wave, err = store.Wave(ctx, project.ID, "wave-two")
	if err != nil || wave.Status != orchestration.WaveCompleted {
		t.Fatalf("completed Wave = %+v err=%v", wave, err)
	}
}

func claimAutopilotMerge(t *testing.T, scheduler *orchestration.Scheduler, projectID int64, claimID string, snapshot supervisor.ProjectSnapshot) orchestration.ClaimDecision {
	t.Helper()
	decision, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{
		ProjectID: projectID, ClaimID: claimID, LeaseOwner: "dashboard-autopilot",
		LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || !decision.Claimed || decision.Intent == nil || decision.Lease == nil {
		t.Fatalf("claim %s = %+v err=%v", claimID, decision, err)
	}
	return decision
}

func seedMaterializedMergeCommand(t *testing.T, db *sql.DB, projectID int64, intent orchestration.Intent, lease orchestration.Lease, commandID, cycleID, baseRef string) workerloop.Command {
	t.Helper()
	commands := workerloop.NewStore(db)
	command, err := commands.CreateCommand(context.Background(), workerloop.CreateCommandInput{
		ID: commandID, ProjectID: projectID, IssueNumber: intent.IssueNumber,
		IdentityKey: commandID + "-identity", Role: "lead", Action: "dispatch",
		ResourceProfile: orchestration.ContinuousResourceProfile, ContextHash: commandID + "-context",
		ExpectedHead: intent.ExpectedHead, Status: workerloop.CommandAwaitingResult,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,status,
		workflow_command_id,context_hash,prompt_hash,created_at,updated_at
	) VALUES(?,?,?,?,?,?,'materialized',?,?,?,?,?)`,
		"autocmd-"+commandID, projectID, intent.ID, lease.ID, "provision-"+commandID, intent.LaneKey,
		command.ID, commandID+"-context", commandID+"-prompt", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO merge_cycle_readbacks(
		id,project_id,intent_id,workflow_command_id,repository,issue_number,pr_number,approved_head,
		expected_base_ref,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,'pending',?,?)`,
		cycleID, projectID, intent.ID, command.ID, intent.Repository, intent.IssueNumber, intent.PRNumber,
		intent.ExpectedHead, baseRef, now, now); err != nil {
		t.Fatal(err)
	}
	return command
}

func acceptedMergeResult(projectID int64, issueNumber int, commentID int64, commandID, commit string) workerloop.Result {
	acceptedAt := time.Now().UTC()
	return workerloop.Result{
		ProjectID: projectID, GitHubCommentID: commentID, IssueNumber: issueNumber,
		CommandID: commandID, Role: "lead", Result: "merged", PayloadHash: "payload-" + commit,
		ValidationStatus: workerloop.ValidationAccepted, AcceptedAt: &acceptedAt, ObservedAt: acceptedAt,
	}
}

func mergedPayload(issueNumber, prNumber int, commandID, head, commit string) workerloop.MarkerPayload {
	return workerloop.MarkerPayload{
		Version: 2, Role: "lead", Result: "merged", CommandID: commandID,
		Repository: "NordCoder/app", Issue: issueNumber, PR: prNumber, ApprovedHead: head, MergeCommit: commit,
	}
}

func assertNextWaveCount(t *testing.T, store *orchestration.Store, projectID int64, expected int) {
	t.Helper()
	intents, err := store.ListIntents(context.Background(), projectID, "")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, intent := range intents {
		if intent.ActionType == orchestration.ActionPlanNextWave {
			count++
		}
	}
	if count != expected {
		t.Fatalf("next-Wave Intent count = %d, want %d: %+v", count, expected, intents)
	}
}
