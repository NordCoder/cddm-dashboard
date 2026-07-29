package orchestration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

const mergeCommit = "441401d9f5c1fb2004eeb19ec612323f74b57199"

func TestMergedResultWaitsForReadbackThenCompletesWaveExactlyOnce(t *testing.T) {
	fixture := newMergeResultFixture(t)
	payload := fixture.mergedPayload(mergeCommit)
	result := fixture.acceptedResult(3001, mergeCommit)

	fixture.facts.value = supervisor.MergeFacts{
		Repository: "NordCoder/app", IssueNumber: 101, IssueState: "open",
		PRNumber: 150, PRState: "open", ApprovedHead: testHead, BaseRef: "main",
	}
	if err := fixture.engine.ReconcileCommandResult(context.Background(), fixture.command, result, payload); err != nil {
		t.Fatal(err)
	}
	assertMergeResultState(t, fixture, orchestration.IntentClaimed, orchestration.LeaseActive, orchestration.AutonomousMaterializationMaterialized, orchestration.MergeCyclePending)
	var pendingReason string
	if err := fixture.db.QueryRow(`SELECT reason_code FROM merge_cycle_readbacks WHERE intent_id=?`, fixture.intent.ID).Scan(&pendingReason); err != nil || pendingReason != "merge_not_visible" {
		t.Fatalf("pending reason = %q err=%v", pendingReason, err)
	}

	mergedAt := time.Now().UTC()
	fixture.facts.value = supervisor.MergeFacts{
		Repository: "NordCoder/app", IssueNumber: 101, IssueState: "closed", IssueLabels: []string{"status:done"},
		PRNumber: 150, PRState: "closed", Merged: true, ApprovedHead: testHead, BaseRef: "main",
		MergeCommit: mergeCommit, MergedAt: &mergedAt,
	}
	if err := fixture.engine.ReconcileCommandResult(context.Background(), fixture.command, result, payload); err != nil {
		t.Fatal(err)
	}
	assertMergeResultState(t, fixture, orchestration.IntentCompleted, orchestration.LeaseCompleted, orchestration.AutonomousMaterializationCompleted, orchestration.MergeCycleVerified)
	assertCompletedWaveAndNextIntent(t, fixture)

	if err := fixture.engine.ReconcileCommandResult(context.Background(), fixture.command, result, payload); err != nil {
		t.Fatal(err)
	}
	assertCompletedWaveAndNextIntent(t, fixture)
}

func TestConflictingMergeCommitFailsClosedWithProjectHold(t *testing.T) {
	fixture := newMergeResultFixture(t)
	observedCommit := "541401d9f5c1fb2004eeb19ec612323f74b57199"
	mergedAt := time.Now().UTC()
	fixture.facts.value = supervisor.MergeFacts{
		Repository: "NordCoder/app", IssueNumber: 101, IssueState: "closed", IssueLabels: []string{"status:done"},
		PRNumber: 150, PRState: "closed", Merged: true, ApprovedHead: testHead, BaseRef: "main",
		MergeCommit: observedCommit, MergedAt: &mergedAt,
	}
	payload := fixture.mergedPayload(mergeCommit)
	result := fixture.acceptedResult(3002, mergeCommit)
	if err := fixture.engine.ReconcileCommandResult(context.Background(), fixture.command, result, payload); err != nil {
		t.Fatal(err)
	}
	assertMergeResultState(t, fixture, orchestration.IntentAmbiguous, orchestration.LeaseSuperseded, orchestration.AutonomousMaterializationAmbiguous, orchestration.MergeCycleAmbiguous)

	intents, err := fixture.store.ListIntents(context.Background(), fixture.project.ID, orchestration.IntentBlocked)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].ActionType != orchestration.ActionHold || intents[0].IssueNumber != 0 || intents[0].ReasonCode != "merge_readback_commit_mismatch" {
		t.Fatalf("project holds = %+v", intents)
	}
	wave, err := fixture.store.Wave(context.Background(), fixture.project.ID, "wave-merge")
	if err != nil || wave.Status != orchestration.WaveBlocked {
		t.Fatalf("blocked Wave = %+v err=%v", wave, err)
	}
}

type mergeResultFixture struct {
	db      *sql.DB
	project supervisor.Project
	store   *orchestration.Store
	engine  *orchestration.MergeAutopilotEngine
	facts   *mergeFactsStub
	intent  orchestration.Intent
	lease   orchestration.Lease
	command workerloop.Command
}

func newMergeResultFixture(t *testing.T) mergeResultFixture {
	t.Helper()
	ctx := context.Background()
	db, project := testProject(t, ":memory:")
	store := orchestration.NewStore(db)
	if _, err := store.UpdateProjectProfile(ctx, orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		DeliveryMode: "auto", MaxActiveWorkUnits: 2, MaxParallelImplementors: 2, MaxParallelQA: 2,
	}); err != nil {
		t.Fatal(err)
	}
	sourceCommand, sourceResultID := seedLeadResult(t, db, project.ID)
	lane := fmt.Sprintf("project:%d:lead", project.ID)
	created, err := store.CreateBatch(ctx, &orchestration.WaveInput{
		ProjectID: project.ID, WaveID: "wave-merge", ControlIssueNumber: 90,
		SourceCommandID: sourceCommand.ID, Status: orchestration.WavePlanned, Issues: []int{101},
	}, []orchestration.IntentInput{{
		ID: "intent-merge-101", ProjectID: project.ID, SourceResultCommentID: sourceResultID,
		SourceCommandID: sourceCommand.ID, ActionID: "merge-101", ActionType: orchestration.ActionMerge,
		Repository: "NordCoder/app", IssueNumber: 101, Role: "lead", PRNumber: 150,
		ExpectedHead: testHead, WaveID: "wave-merge", Priority: 20, LaneKey: lane, Status: orchestration.IntentPending,
	}})
	if err != nil || len(created) != 1 {
		t.Fatalf("create merge Intent = %+v err=%v", created, err)
	}
	snapshot := supervisor.ProjectSnapshot{Project: project, Issues: []supervisor.Issue{
		{Number: 90, State: "open"},
		{Number: 101, State: "open", PullRequests: []supervisor.PullRequest{{Number: 150, State: "open", BaseRef: "main", HeadSHA: testHead}}},
	}}
	scheduler := orchestration.NewScheduler(store)
	decision, err := scheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "merge-claim", LeaseOwner: "dashboard-autopilot",
		LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || !decision.Claimed || decision.Lease == nil {
		t.Fatalf("claim merge Intent = %+v err=%v", decision, err)
	}
	commands := workerloop.NewStore(db)
	command, err := commands.CreateCommand(ctx, workerloop.CreateCommandInput{
		ID: "cmd-merge-101", ProjectID: project.ID, IssueNumber: 101, IdentityKey: "merge-command-101",
		Role: "lead", Action: "dispatch", ResourceProfile: orchestration.ContinuousResourceProfile,
		ContextHash: "merge-context", ExpectedHead: testHead, Status: workerloop.CommandAwaitingResult,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	materializationID := "autocmd-merge-101"
	if _, err := db.Exec(`INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,status,
		workflow_command_id,context_hash,prompt_hash,created_at,updated_at
	) VALUES(?,?,?,?,?,?,'materialized',?,?,?,?,?)`,
		materializationID, project.ID, created[0].ID, decision.Lease.ID, "provision-merge-101", lane,
		command.ID, "merge-context", "merge-prompt", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO merge_cycle_readbacks(
		id,project_id,intent_id,workflow_command_id,repository,issue_number,pr_number,approved_head,
		expected_base_ref,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,'pending',?,?)`,
		"merge-cycle-101", project.ID, created[0].ID, command.ID, "NordCoder/app", 101, 150, testHead, "main", now, now); err != nil {
		t.Fatal(err)
	}
	facts := &mergeFactsStub{}
	engine, err := orchestration.NewMergeAutopilotEngine(
		store, scheduler, provisioningService(t, store), mergePlannerStub{}, mergeDeliveryStub{},
		delivery.UnavailableBindingResolver{}, supervisor.NewStore(db), facts,
	)
	if err != nil {
		t.Fatal(err)
	}
	return mergeResultFixture{
		db: db, project: project, store: store, engine: engine, facts: facts,
		intent: created[0], lease: *decision.Lease, command: command,
	}
}

func (f mergeResultFixture) mergedPayload(commit string) workerloop.MarkerPayload {
	return workerloop.MarkerPayload{
		Version: 2, Role: "lead", Result: "merged", CommandID: f.command.ID,
		Repository: "NordCoder/app", Issue: 101, PR: 150, ApprovedHead: testHead, MergeCommit: commit,
	}
}

func (f mergeResultFixture) acceptedResult(commentID int64, commit string) workerloop.Result {
	acceptedAt := time.Now().UTC()
	return workerloop.Result{
		ProjectID: f.project.ID, GitHubCommentID: commentID, IssueNumber: 101,
		CommandID: f.command.ID, Role: "lead", Result: "merged", PayloadHash: "payload-" + commit,
		ValidationStatus: workerloop.ValidationAccepted, AcceptedAt: &acceptedAt, ObservedAt: acceptedAt,
	}
}

func assertMergeResultState(t *testing.T, fixture mergeResultFixture, intentStatus, leaseStatus, materializationStatus, cycleStatus string) {
	t.Helper()
	intent, err := fixture.store.Intent(context.Background(), fixture.intent.ID)
	if err != nil || intent.Status != intentStatus {
		t.Fatalf("Intent = %+v err=%v, want %s", intent, err, intentStatus)
	}
	lease, err := orchestration.NewScheduler(fixture.store).Lease(context.Background(), fixture.project.ID, fixture.lease.ID)
	if err != nil || lease.Status != leaseStatus {
		t.Fatalf("lease = %+v err=%v, want %s", lease, err, leaseStatus)
	}
	var storedMaterialization, storedCycle string
	if err := fixture.db.QueryRow(`SELECT status FROM autonomous_command_materializations WHERE intent_id=?`, fixture.intent.ID).Scan(&storedMaterialization); err != nil || storedMaterialization != materializationStatus {
		t.Fatalf("materialization = %s err=%v, want %s", storedMaterialization, err, materializationStatus)
	}
	if err := fixture.db.QueryRow(`SELECT status FROM merge_cycle_readbacks WHERE intent_id=?`, fixture.intent.ID).Scan(&storedCycle); err != nil || storedCycle != cycleStatus {
		t.Fatalf("merge cycle = %s err=%v, want %s", storedCycle, err, cycleStatus)
	}
}

func assertCompletedWaveAndNextIntent(t *testing.T, fixture mergeResultFixture) {
	t.Helper()
	wave, err := fixture.store.Wave(context.Background(), fixture.project.ID, "wave-merge")
	if err != nil || wave.Status != orchestration.WaveCompleted {
		t.Fatalf("Wave = %+v err=%v", wave, err)
	}
	var status, commit string
	if err := fixture.db.QueryRow(`SELECT status,merge_commit_sha FROM workflow_wave_issues WHERE project_id=? AND wave_id='wave-merge' AND issue_number=101`, fixture.project.ID).Scan(&status, &commit); err != nil || status != orchestration.WaveIssueDone || commit != mergeCommit {
		t.Fatalf("Wave item = %s/%s err=%v", status, commit, err)
	}
	intents, err := fixture.store.ListIntents(context.Background(), fixture.project.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	next := make([]orchestration.Intent, 0, 1)
	for _, intent := range intents {
		if intent.ActionType == orchestration.ActionPlanNextWave {
			next = append(next, intent)
		}
	}
	if len(next) != 1 || next[0].WaveID != "wave-merge" || next[0].IssueNumber != 90 || next[0].Role != "lead" || next[0].Status != orchestration.IntentPending {
		t.Fatalf("next-Wave Intents = %+v", next)
	}
}

type mergeFactsStub struct {
	value supervisor.MergeFacts
	err   error
}

func (s *mergeFactsStub) ReadMergeFacts(context.Context, string, string, int, int) (supervisor.MergeFacts, error) {
	return s.value, s.err
}

type mergePlannerStub struct{}

func (mergePlannerStub) GenerateAutonomousIntent(context.Context, int64, int, string, string, string) (planning.GenerationResult, error) {
	return planning.GenerationResult{}, nil
}

func (mergePlannerStub) Get(context.Context, int64, int, int64) (planning.GenerationResult, error) {
	return planning.GenerationResult{}, nil
}

type mergeDeliveryStub struct{}

func (mergeDeliveryStub) Create(context.Context, int64, int, delivery.Confirmation) (delivery.Command, error) {
	return delivery.Command{}, nil
}
