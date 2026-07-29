package orchestration_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func claimRestartAutopilot(t *testing.T, scheduler *orchestration.Scheduler, projectID int64, claimID string, snapshot supervisor.ProjectSnapshot) orchestration.ClaimDecision {
	t.Helper()
	decision, err := scheduler.ClaimNext(context.Background(), orchestration.ClaimRequest{
		ProjectID: projectID, ClaimID: claimID, LeaseOwner: "dashboard-autopilot", LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || !decision.Claimed || decision.Intent == nil || decision.Lease == nil {
		t.Fatalf("Autopilot claim %s = %+v err=%v", claimID, decision, err)
	}
	return decision
}

func TestContinuousAutopilotRestartRecoveryPreservesExactDurableIdentityGraph(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "autopilot-soak.db")
	db, project, store, scheduler, _, sourceCommandID := schedulerFixture(t, path, 3, 3, 1)
	if _, err := store.UpdateProjectProfile(ctx, orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		DeliveryMode: "auto", MaxActiveWorkUnits: 3, MaxParallelImplementors: 3, MaxParallelQA: 1,
		ChatGPTProjectURL: finalProjectURL,
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := persistRestartSoakSnapshot(t, db, project)
	wave := &orchestration.WaveInput{
		ProjectID: project.ID, WaveID: "wave-restart-soak", ControlIssueNumber: 90,
		SourceCommandID: sourceCommandID, Status: orchestration.WaveActive, Issues: []int{101, 102, 104, 105},
	}
	pending := soakIntent(project.ID, sourceCommandID, "pending", 101, "implementor", fmt.Sprintf("project:%d:issue:101:implementor", project.ID), wave.WaveID, 50)
	claimed := soakIntent(project.ID, sourceCommandID, "claimed", 102, "implementor", fmt.Sprintf("project:%d:issue:102:implementor", project.ID), wave.WaveID, 10)
	provisioned := soakIntent(project.ID, sourceCommandID, "provisioned", 90, "lead", fmt.Sprintf("project:%d:lead", project.ID), wave.WaveID, 20)
	provisioned.ActionType = orchestration.ActionPlanNextWave
	deliveryPending := soakIntent(project.ID, sourceCommandID, "delivery", 104, "implementor", fmt.Sprintf("project:%d:issue:104:implementor", project.ID), wave.WaveID, 30)
	deliveryPending.PRNumber, deliveryPending.ExpectedHead = 154, otherHead
	awaitingResult := soakIntent(project.ID, sourceCommandID, "awaiting", 105, "qa", fmt.Sprintf("project:%d:issue:105:qa:head", project.ID), wave.WaveID, 40)
	awaitingResult.PRNumber, awaitingResult.ExpectedHead = 155, testHead
	inputs := []orchestration.IntentInput{pending, claimed, provisioned, deliveryPending, awaitingResult}
	created, err := store.CreateBatch(ctx, wave, append([]orchestration.IntentInput(nil), inputs...))
	if err != nil || len(created) != len(inputs) {
		t.Fatalf("create restart fixture = %+v err=%v", created, err)
	}
	if replayed, err := store.CreateBatch(ctx, wave, append([]orchestration.IntentInput(nil), inputs...)); err != nil || len(replayed) != len(inputs) {
		t.Fatalf("idempotent action replay = %+v err=%v", replayed, err)
	}

	claimedDecision := claimRestartAutopilot(t, scheduler, project.ID, "restart-claimed", snapshot)
	if claimedDecision.Intent.ID != claimed.ID {
		t.Fatalf("claimed stage = %+v", claimedDecision.Intent)
	}
	provisionedDecision := claimRestartAutopilot(t, scheduler, project.ID, "restart-provisioned", snapshot)
	if provisionedDecision.Intent.ID != provisioned.ID {
		t.Fatalf("provisioned stage = %+v", provisionedDecision.Intent)
	}
	deliveryDecision := claimRestartAutopilot(t, scheduler, project.ID, "restart-delivery", snapshot)
	if deliveryDecision.Intent.ID != deliveryPending.ID {
		t.Fatalf("delivery stage = %+v", deliveryDecision.Intent)
	}
	awaitingDecision := claimRestartAutopilot(t, scheduler, project.ID, "restart-awaiting", snapshot)
	if awaitingDecision.Intent.ID != awaitingResult.ID {
		t.Fatalf("awaiting stage = %+v", awaitingDecision.Intent)
	}
	blocked, err := scheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "restart-pending-blocked", LeaseOwner: "dashboard-autopilot", LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || blocked.Claimed || blocked.Reason != "no_runnable_intent" {
		t.Fatalf("pending stage must remain pending at saturated WIP = %+v err=%v", blocked, err)
	}

	provisions := provisioningService(t, store)
	claimedRequest, claimedProvision := claimSoakProvision(t, provisions, project.ID, *claimedDecision.Lease, "claimed")
	bindings := browserbinding.New(db, time.Minute)
	finalizer, err := orchestration.NewProvisioningFinalizer(store, bindings)
	if err != nil {
		t.Fatal(err)
	}
	finalizeSoakProvision(t, provisions, finalizer, bindings, project.ID, *provisionedDecision.Lease, "provisioned", 71)
	deliveryRequest := finalizeSoakProvision(t, provisions, finalizer, bindings, project.ID, *deliveryDecision.Lease, "delivery", 72)
	awaitingRequest := finalizeSoakProvision(t, provisions, finalizer, bindings, project.ID, *awaitingDecision.Lease, "awaiting", 73)

	plans := map[int]planning.GenerationResult{
		104: restartGeneration(project.ID, 104, "implementor", otherHead, 1004),
		105: restartGeneration(project.ID, 105, "qa", testHead, 1005),
	}
	engine, coordinator := restartProductionRuntime(t, db, store, scheduler, provisions, bindings, plans)
	if err := engine.ReconcileProject(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	deliveryChain := readSoakCommandChain(t, db, project.ID, deliveryPending.ID)
	awaitingChain := readSoakCommandChain(t, db, project.ID, awaitingResult.ID)
	completeSoakDelivery(t, coordinator, awaitingRequest, "session-awaiting")
	awaitingChain = readSoakCommandChain(t, db, project.ID, awaitingResult.ID)

	observedResult := workerloop.Result{
		ProjectID: project.ID, GitHubCommentID: 99002, IssueNumber: awaitingResult.IssueNumber,
		CommandID: awaitingChain.WorkflowID, Role: awaitingResult.Role, Result: "changes_required",
		Payload: []byte(fmt.Sprintf(`{"version":2,"command_id":%q,"reviewed_head":%q}`, awaitingChain.WorkflowID, testHead)),
		PayloadHash: "restart-result-evidence", ValidationStatus: workerloop.ValidationMalformed,
		ValidationReason: "non_terminal_observation", ObservedAt: time.Now().UTC(),
	}
	if err := workerloop.NewStore(db).UpsertResult(ctx, observedResult); err != nil {
		t.Fatal(err)
	}

	assertRestartFixtureCardinality(t, db, project.ID)
	before := captureRestartIdentityGraph(t, db, project.ID)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	afterOpen := captureRestartIdentityGraph(t, reopened, project.ID)
	if !reflect.DeepEqual(afterOpen, before) {
		t.Fatalf("identity graph changed across close/reopen\nbefore=%+v\nafter=%+v", before, afterOpen)
	}

	reopenedStore := orchestration.NewStore(reopened)
	reopenedScheduler := orchestration.NewScheduler(reopenedStore)
	reopenedProvisions := provisioningService(t, reopenedStore)
	restartedBindings := browserbinding.New(reopened, time.Minute)
	restartedEngine, restartedCoordinator := restartProductionRuntime(t, reopened, reopenedStore, reopenedScheduler, reopenedProvisions, restartedBindings, plans)
	if err := restartedEngine.ReconcileProject(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	for _, replay := range []struct {
		issue int
		chain soakCommandChain
	}{{104, deliveryChain}, {105, awaitingChain}} {
		command, err := restartedCoordinator.Create(ctx, project.ID, replay.issue, replay.chain.Confirmation)
		if err != nil || command.ID != replay.chain.DeliveryID {
			t.Fatalf("restart production delivery replay for Issue #%d = %+v err=%v", replay.issue, command, err)
		}
		current := readSoakCommandChain(t, reopened, project.ID, replay.chain.IntentID)
		if current != replay.chain {
			t.Fatalf("restart production command chain changed for Issue #%d: before=%+v after=%+v", replay.issue, replay.chain, current)
		}
	}

	duplicateClaim, err := reopenedScheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "restart-claimed", LeaseOwner: "dashboard-autopilot", LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || !duplicateClaim.Claimed || duplicateClaim.Lease == nil || duplicateClaim.Lease.ID != claimedDecision.Lease.ID {
		t.Fatalf("restart duplicate scheduler claim = %+v err=%v", duplicateClaim, err)
	}
	duplicateProvision, err := reopenedProvisions.ClaimNext(ctx, orchestration.ProvisionClaimInput{
		ClaimID: "provision-claimed", ClaimOwner: "extension-claimed", ClaimTTL: time.Minute,
	})
	if err != nil || duplicateProvision == nil || duplicateProvision.ID != claimedRequest.ID || duplicateProvision.ClaimToken != claimedProvision.ClaimToken {
		t.Fatalf("restart duplicate provisioning claim = %+v err=%v", duplicateProvision, err)
	}
	if err := workerloop.NewStore(reopened).UpsertResult(ctx, observedResult); err != nil {
		t.Fatalf("restart duplicate result/comment = %v", err)
	}

	afterRecovery := captureRestartIdentityGraph(t, reopened, project.ID)
	if !reflect.DeepEqual(afterRecovery, before) {
		t.Fatalf("reconciliation manufactured or retargeted durable work\nbefore=%+v\nafter=%+v", before, afterRecovery)
	}

	status, err := orchestration.NewOperationsService(reopenedStore).Status(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.ProjectID != project.ID || status.ActiveWave == nil || status.ActiveWave.WaveID != wave.WaveID || status.ActiveWave.SourceCommandID != sourceCommandID || !reflect.DeepEqual(status.ActiveWave.Issues, wave.Issues) {
		t.Fatalf("restart Wave/Project projection = %+v", status.ActiveWave)
	}
	if len(status.Intents) != 5 || len(status.Leases) != 4 || len(status.Results) != 1 || status.Results[0].GitHubCommentID != observedResult.GitHubCommentID {
		t.Fatalf("restart complete projection = intents=%d leases=%d results=%+v", len(status.Intents), len(status.Leases), status.Results)
	}
	if status.Counts.PendingIntents != 1 || status.Counts.ClaimedIntents != 4 || status.Counts.ActiveLeases != 4 || len(status.Queue) != 5 {
		t.Fatalf("restart queue projection = counts=%+v queue=%d", status.Counts, len(status.Queue))
	}
	if status.Counts.ManagedSessions != 3 || status.Counts.ActiveCommands != 2 {
		t.Fatalf("restart execution projection = %+v", status.Counts)
	}
	seenCommands := map[string]orchestration.CommandProjection{}
	for _, command := range status.Commands {
		seenCommands[command.WorkflowCommandID] = command
	}
	for _, chain := range []soakCommandChain{deliveryChain, awaitingChain} {
		command, ok := seenCommands[chain.WorkflowID]
		if !ok || command.MaterializationID != chain.MaterializationID || command.IntentID != chain.IntentID || command.LeaseID != chain.LeaseID || command.DeliveryCommandID != chain.DeliveryID || command.WorkerSessionID == "" || command.ProvisionRequestID == "" {
			t.Fatalf("restart command identity %s = %+v", chain.WorkflowID, command)
		}
	}
	qaCommand := seenCommands[awaitingChain.WorkflowID]
	if qaCommand.IssueNumber != 105 || qaCommand.Role != "qa" || qaCommand.ExpectedHead != testHead {
		t.Fatalf("exact-Head QA command chain = %+v", qaCommand)
	}
	_ = deliveryRequest
}
