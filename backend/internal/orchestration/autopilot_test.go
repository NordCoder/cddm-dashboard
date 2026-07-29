package orchestration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

func TestAutopilotMaterializesOneCommandAndCompletesExactLease(t *testing.T) {
	ctx := context.Background()
	db, project, store, scheduler, snapshot, sourceCommandID := schedulerFixture(t, ":memory:", 2, 2, 2)
	if _, err := store.UpdateProjectProfile(ctx, orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		DeliveryMode: "auto", MaxActiveWorkUnits: 2, MaxParallelImplementors: 2, MaxParallelQA: 2,
		ChatGPTProjectURL: finalProjectURL,
	}); err != nil {
		t.Fatal(err)
	}
	lane := fmt.Sprintf("project:%d:issue:101:implementor", project.ID)
	intent := schedulerIntent(project.ID, sourceCommandID, "autopilot-implement", orchestration.ActionDispatch, 101, "implementor", 50, lane)
	intent.ExpectedHead = testHead
	createSchedulerIntents(t, store, project.ID, sourceCommandID, []orchestration.IntentInput{intent})
	decision, err := scheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "autopilot-claim", LeaseOwner: "dashboard-autopilot",
		LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || !decision.Claimed || decision.Lease == nil {
		t.Fatalf("claim = %+v err=%v", decision, err)
	}

	provisions := provisioningService(t, store)
	request, err := provisions.Enqueue(ctx, orchestration.EnqueueProvisioningInput{
		ProjectID: project.ID, LeaseID: decision.Lease.ID,
		LeaseOwner: decision.Lease.LeaseOwner, LeaseToken: decision.Lease.LeaseToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := provisions.ClaimNext(ctx, orchestration.ProvisionClaimInput{
		ClaimID: "extension-claim", ClaimOwner: "extension", ClaimTTL: time.Minute,
	})
	if err != nil || claimed == nil {
		t.Fatalf("provision claim = %+v err=%v", claimed, err)
	}
	if _, err := provisions.Complete(ctx, orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: claimed.ClaimToken,
		Outcome: orchestration.ProvisionSurfaceReady, WorkerID: "autopilot-worker", TabID: 77,
	}); err != nil {
		t.Fatal(err)
	}
	if err := persistFinalizeSnapshot(t, db, project.ID, testHead); err != nil {
		t.Fatal(err)
	}
	bindings := browserbinding.New(db, time.Minute)
	target := browserbinding.TargetRef{Kind: browserbinding.TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/autopilot"}
	if _, err := bindings.Register(ctx, browserbinding.RegisterInput{
		WorkerID: "autopilot-worker", SessionID: "autopilot-session", ProtocolVersion: "m12-c1",
		Capabilities: []string{"managed_exact_tab", "library_bootstrap"},
		Observation:  browserbinding.Observation{Target: &target},
	}); err != nil {
		t.Fatal(err)
	}
	finalizer, err := orchestration.NewProvisioningFinalizer(store, bindings)
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := finalizer.Finalize(ctx, orchestration.FinalizeProvisioningInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: claimed.ClaimToken,
		WorkerID: "autopilot-worker", TabID: 77, Target: target,
		ObservedChatGPTURL: "https://chatgpt.com/g/g-project/repository/c/autopilot",
		AttachmentEvidence: append([]string(nil), request.Attachments...),
	})
	if err != nil {
		t.Fatal(err)
	}

	canonicalLane := "NordCoder/app#101:implementor"
	plan := planning.GenerationResult{
		Status: planning.StatusFallback, PlanID: 41, CreatedAt: time.Now().UTC(),
		Context: planning.PromptContext{
			Version:     planning.PromptContextVersion,
			Repository:  planning.RepositoryIdentity{ProjectID: project.ID, Owner: "NordCoder", Repository: "app", WorkflowMode: "pull_request"},
			Issue:       planning.IssueIdentity{Number: 101, Title: "Candidate", Lifecycle: "implementation"},
			CurrentHead: testHead, ContextHash: "autopilot-context",
			Route: workflow.Route{Action: "dispatch", TargetRole: "implementor", LaneKey: canonicalLane, ExpectedHead: testHead},
		},
		Plan: &planning.PromptPlan{
			Version: planning.PromptPlanVersion, Action: "dispatch", TargetRole: "implementor",
			LaneKey: canonicalLane, ExpectedHead: testHead, Prompt: "bounded autonomous prompt",
		},
		PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, ContextHash: "autopilot-context", PlanHash: "autopilot-plan-hash"},
	}
	planner := &fixedAutonomousPlanner{result: plan}
	deliveryRecorder := &recordingAutonomousDelivery{db: db, commands: workerloop.NewStore(db), result: plan}
	engine, err := orchestration.NewAutopilotEngine(
		store, scheduler, provisions, planner, deliveryRecorder,
		delivery.NewBrowserBindingResolver(bindings), supervisor.NewStore(db),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ReconcileProject(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := engine.ReconcileProject(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if deliveryRecorder.calls != 1 {
		t.Fatalf("delivery calls = %d, want one", deliveryRecorder.calls)
	}

	var materializationStatus, workflowCommandID, deliveryCommandID, authorityKind, authorityRef string
	if err := db.QueryRowContext(ctx, `SELECT status,workflow_command_id,delivery_command_id FROM autonomous_command_materializations WHERE intent_id=?`, intent.ID).Scan(&materializationStatus, &workflowCommandID, &deliveryCommandID); err != nil {
		t.Fatal(err)
	}
	if materializationStatus != orchestration.AutonomousMaterializationMaterialized || workflowCommandID == "" || deliveryCommandID == "" {
		t.Fatalf("materialization = %s %s %s", materializationStatus, workflowCommandID, deliveryCommandID)
	}
	if err := db.QueryRowContext(ctx, `SELECT authority_kind,authority_ref FROM delivery_commands WHERE id=?`, deliveryCommandID).Scan(&authorityKind, &authorityRef); err != nil {
		t.Fatal(err)
	}
	if authorityKind != delivery.AuthorityAutonomousIntent || authorityRef != intent.ID {
		t.Fatalf("delivery authority = %s/%s", authorityKind, authorityRef)
	}
	resolver := orchestration.NewDeliveryBindingResolver(db, delivery.NewBrowserBindingResolver(bindings))
	resolved, err := resolver.Resolve(ctx, project.ID, canonicalLane)
	if err != nil || resolved.BindingID != finalized.BoundBindingID || resolved.LaneKey != canonicalLane {
		t.Fatalf("resolved autonomous binding = %+v err=%v", resolved, err)
	}

	command, err := workerloop.NewStore(db).GetCommand(ctx, workflowCommandID)
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Now().UTC()
	if err := engine.ReconcileCommandResult(ctx, command, workerloop.Result{
		ProjectID: project.ID, CommandID: command.ID, Role: "implementor", Result: "candidate_ready",
		ValidationStatus: workerloop.ValidationAccepted, AcceptedAt: &acceptedAt,
	}, workerloop.MarkerPayload{Version: 2, Role: "implementor", Result: "candidate_ready", CommandID: command.ID, PR: 150, Head: testHead}); err != nil {
		t.Fatal(err)
	}
	lease, err := scheduler.Lease(ctx, project.ID, decision.Lease.ID)
	if err != nil || lease.Status != orchestration.LeaseCompleted {
		t.Fatalf("completed lease = %+v err=%v", lease, err)
	}
	storedIntent, err := store.Intent(ctx, intent.ID)
	if err != nil || storedIntent.Status != orchestration.IntentCompleted {
		t.Fatalf("completed Intent = %+v err=%v", storedIntent, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM autonomous_command_materializations WHERE intent_id=?`, intent.ID).Scan(&materializationStatus); err != nil || materializationStatus != orchestration.AutonomousMaterializationCompleted {
		t.Fatalf("completed materialization = %s err=%v", materializationStatus, err)
	}
}

type fixedAutonomousPlanner struct {
	result planning.GenerationResult
	calls  int
}

func (p *fixedAutonomousPlanner) GenerateAutonomous(context.Context, int64, int, string, string) (planning.GenerationResult, error) {
	p.calls++
	return p.result, nil
}

func (p *fixedAutonomousPlanner) Get(context.Context, int64, int, int64) (planning.GenerationResult, error) {
	return p.result, nil
}

type recordingAutonomousDelivery struct {
	db       *sql.DB
	commands *workerloop.Store
	result   planning.GenerationResult
	calls    int
}

func (r *recordingAutonomousDelivery) Create(ctx context.Context, projectID int64, issueNumber int, confirmation delivery.Confirmation) (delivery.Command, error) {
	r.calls++
	const deliveryID = "delivery-autopilot"
	const workflowID = "cmd-autopilot"
	now := time.Now().UTC()
	workflowCommand, err := r.commands.CreateCommand(ctx, workerloop.CreateCommandInput{
		ID: workflowID, ProjectID: projectID, IssueNumber: issueNumber, IdentityKey: "autopilot-command-identity",
		Role: "implementor", Action: "dispatch", ResourceProfile: orchestration.ContinuousResourceProfile,
		ContextHash: confirmation.ExpectedContextHash, ExpectedHead: confirmation.ExpectedHead,
		Status: workerloop.CommandDeliveryPending,
	})
	if err != nil {
		return delivery.Command{}, err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO delivery_commands(
		id,project_id,issue_number,plan_id,idempotency_key,confirmation_fingerprint,plan_hash,context_hash,prompt_hash,prompt_text,
		action,target_role,lane_key,expected_head,binding_id,binding_version,worker_id,worker_session_id,presence_token,target_kind,target_ref,
		status,created_at,expires_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id,issue_number,idempotency_key) DO NOTHING`,
		deliveryID, projectID, issueNumber, confirmation.PlanID, confirmation.IdempotencyKey, "fingerprint",
		confirmation.ExpectedPlanHash, confirmation.ExpectedContextHash, "prompt-hash", "rendered v2 prompt",
		"dispatch", "implementor", r.result.Plan.LaneKey, confirmation.ExpectedHead,
		confirmation.ExpectedBindingID, confirmation.ExpectedBindingVer, "autopilot-worker", "autopilot-session", confirmation.ExpectedPresenceToken,
		browserbinding.TargetKindChatGPTConversation, "https://chatgpt.com/c/autopilot", delivery.StatusPending,
		now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return delivery.Command{}, err
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO workflow_delivery_links(workflow_command_id,delivery_command_id,created_at) VALUES(?,?,?) ON CONFLICT DO NOTHING`, workflowCommand.ID, deliveryID, now.Format(time.RFC3339Nano)); err != nil {
		return delivery.Command{}, err
	}
	return delivery.Command{ID: deliveryID, ProjectID: projectID, IssueNumber: issueNumber, PlanID: confirmation.PlanID, Prompt: "rendered v2 prompt", Status: delivery.StatusPending}, nil
}
