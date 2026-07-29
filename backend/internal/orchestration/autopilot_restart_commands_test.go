package orchestration_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

type soakCommandChain struct {
	IntentID          string
	LeaseID           string
	MaterializationID string
	WorkflowID        string
	DeliveryID        string
	CommandInput      workerloop.CreateCommandInput
}

func insertSoakMaterializedChain(t *testing.T, db *sql.DB, projectID int64, intent orchestration.IntentInput, lease orchestration.Lease, request orchestration.ProvisionRequest, stage, workflowStatus, deliveryStatus string) soakCommandChain {
	t.Helper()
	if request.Target == nil {
		t.Fatalf("%s provisioning has no target", stage)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	workflowID := "cmd-soak-" + stage
	deliveryID := "delivery-soak-" + stage
	materializationID := soakAutopilotID(projectID, intent.ID)
	contextHash := "context-soak-" + stage
	promptHash := "prompt-soak-" + stage
	commandInput := workerloop.CreateCommandInput{
		ID: workflowID, ProjectID: projectID, IssueNumber: intent.IssueNumber,
		IdentityKey: "autopilot:" + intent.ID, Role: intent.Role, Action: intent.ActionType,
		ResourceProfile: orchestration.ContinuousResourceProfile, ContextHash: contextHash,
		ExpectedHead: intent.ExpectedHead, Status: workflowStatus,
	}
	if _, err := workerloop.NewStore(db).CreateCommand(ctx, commandInput); err != nil {
		t.Fatal(err)
	}
	terminalAt, outcomeReason, outcomeEvidence := "", "", ""
	if deliveryStatus == delivery.StatusDelivered {
		terminalAt = now.Format(time.RFC3339Nano)
		outcomeReason = "delivered"
		outcomeEvidence = request.ObservedChatGPTURL
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO delivery_commands(
		id,project_id,issue_number,plan_id,idempotency_key,confirmation_fingerprint,plan_hash,context_hash,prompt_hash,prompt_text,
		action,target_role,lane_key,expected_head,binding_id,binding_version,worker_id,worker_session_id,presence_token,target_kind,target_ref,
		status,created_at,expires_at,terminal_at,outcome_reason,outcome_evidence
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		deliveryID, projectID, intent.IssueNumber, int64(1000+intent.IssueNumber), "autopilot:"+intent.ID,
		"fingerprint-"+stage, "plan-hash-"+stage, contextHash, promptHash, "bounded restart soak prompt",
		intent.ActionType, intent.Role, intent.LaneKey, intent.ExpectedHead, request.BoundBindingID, request.BoundBindingVersion,
		request.WorkerID, "session-"+stage, "presence-"+stage, request.Target.Kind, request.Target.Origin+request.Target.Path,
		deliveryStatus, now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), terminalAt, outcomeReason, outcomeEvidence); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_delivery_links(workflow_command_id,delivery_command_id,created_at) VALUES(?,?,?)`, workflowID, deliveryID, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,delivery_lane_key,plan_id,status,context_hash,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?, 'pending',?,?,?)`,
		materializationID, projectID, intent.ID, lease.ID, request.ID, intent.LaneKey, intent.LaneKey,
		int64(1000+intent.IssueNumber), contextHash, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE autonomous_command_materializations SET
		status='materialized',workflow_command_id=?,delivery_command_id=?,prompt_hash=?,updated_at=? WHERE id=? AND status='pending'`,
		workflowID, deliveryID, promptHash, now.Format(time.RFC3339Nano), materializationID); err != nil {
		t.Fatal(err)
	}
	return soakCommandChain{
		IntentID: intent.ID, LeaseID: lease.ID, MaterializationID: materializationID,
		WorkflowID: workflowID, DeliveryID: deliveryID, CommandInput: commandInput,
	}
}

func soakAutopilotID(projectID int64, intentID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", projectID, intentID)))
	return fmt.Sprintf("autocmd-%x", sum[:16])
}
