package orchestration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func TestOperationsProjectionRejectsCrossProjectCommandLinks(t *testing.T) {
	ctx := context.Background()
	db, project, store, scheduler, _, sourceCommandID := schedulerFixture(t, ":memory:", 1, 1, 1)
	if _, err := store.UpdateProjectProfile(ctx, orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		DeliveryMode: "auto", MaxActiveWorkUnits: 1, MaxParallelImplementors: 1, MaxParallelQA: 1,
		ChatGPTProjectURL: finalProjectURL,
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := persistRestartSoakSnapshot(t, db, project)
	input := schedulerIntent(project.ID, sourceCommandID, "projection-isolation", orchestration.ActionDispatch, 101, "implementor", 10, fmt.Sprintf("project:%d:issue:101:implementor", project.ID))
	createSchedulerIntents(t, store, project.ID, sourceCommandID, []orchestration.IntentInput{input})
	claimed := claimRestartAutopilot(t, scheduler, project.ID, "projection-isolation", snapshot)
	if claimed.Lease == nil {
		t.Fatal("expected claimed lease")
	}

	provisions := provisioningService(t, store)
	bindings := browserbinding.New(db, time.Minute)
	finalizer, err := orchestration.NewProvisioningFinalizer(store, bindings)
	if err != nil {
		t.Fatal(err)
	}
	finalized := finalizeSoakProvision(t, provisions, finalizer, bindings, project.ID, *claimed.Lease, "projection-isolation", 91)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	const materializationID = "materialization-project-isolation"
	if _, err := db.ExecContext(ctx, `INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,'pending',?,?)`, materializationID, project.ID, input.ID, claimed.Lease.ID, finalized.ID, input.LaneKey, now, now); err != nil {
		t.Fatal(err)
	}

	result, err := db.ExecContext(ctx, `INSERT INTO projects(owner,repository,workflow_mode,polling_enabled,poll_interval_seconds,created_at,updated_at)
		VALUES('NordCoder','other-project','pull_request',1,60,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	otherProjectID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	commandStore := workerloop.NewStore(db)
	foreignWorkflow, err := commandStore.CreateCommand(ctx, workerloop.CreateCommandInput{
		ProjectID: otherProjectID, IssueNumber: 201, IdentityKey: "foreign-workflow",
		Role: "implementor", Action: "dispatch", ResourceProfile: "cddm-dashboard-resources/v2.0",
		ContextHash: "foreign-workflow-context", Status: workerloop.CommandCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE autonomous_command_materializations SET workflow_command_id=? WHERE id=?`, foreignWorkflow.ID, materializationID); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestration.NewOperationsService(store).Status(ctx, project.ID); err == nil {
		t.Fatal("cross-project Workflow Command link was projected as local evidence")
	}

	localWorkflow, err := commandStore.CreateCommand(ctx, workerloop.CreateCommandInput{
		ProjectID: project.ID, IssueNumber: 101, IdentityKey: "local-workflow",
		Role: "implementor", Action: "dispatch", ResourceProfile: "cddm-dashboard-resources/v2.0",
		ContextHash: "local-workflow-context", Status: workerloop.CommandCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	const foreignDeliveryID = "delivery-cross-project"
	if _, err := db.ExecContext(ctx, `INSERT INTO delivery_commands(
		id,project_id,issue_number,plan_id,idempotency_key,confirmation_fingerprint,plan_hash,context_hash,
		prompt_hash,prompt_text,action,target_role,lane_key,expected_head,binding_id,binding_version,
		worker_id,worker_session_id,presence_token,target_kind,target_ref,status,created_at,expires_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		foreignDeliveryID, otherProjectID, 201, 1, "foreign-delivery", "foreign-fingerprint", "foreign-plan",
		"foreign-context", "foreign-prompt", "bounded prompt", "dispatch", "implementor", "foreign-lane", "",
		"foreign-binding", 1, "foreign-worker", "foreign-session", "foreign-presence", "chatgpt_conversation",
		"https://chatgpt.com/c/foreign", "pending", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE autonomous_command_materializations SET workflow_command_id=?,delivery_command_id=? WHERE id=?`, localWorkflow.ID, foreignDeliveryID, materializationID); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestration.NewOperationsService(store).Status(ctx, project.ID); err == nil {
		t.Fatal("cross-project delivery command link was projected as local evidence")
	}
}
