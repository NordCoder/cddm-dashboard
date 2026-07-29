package orchestration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
)

func TestFinalizeProvisioningSupersedesPriorFreshRoleSession(t *testing.T) {
	fixture := newFinalizeFixture(t, testHead)
	ctx := context.Background()
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	oldStamp := now.Add(-time.Minute).Format(time.RFC3339Nano)

	var sourceCommandID, repository string
	if err := fixture.db.QueryRowContext(ctx, `SELECT source_command_id,repository FROM workflow_intents WHERE id=?`, fixture.request.IntentID).Scan(&sourceCommandID, &repository); err != nil {
		t.Fatal(err)
	}
	oldIntentID := "old-qa-intent"
	oldLane := "project:1:issue:101:qa:old-head"
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO workflow_intents(
		id,project_id,source_result_comment_id,source_command_id,action_id,action_type,repository,issue_number,
		role,pr_number,expected_head,expected_previous_head,reason_code,decision_category,wave_id,priority,
		lane_key,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, oldIntentID, fixture.project.ID, 7001, sourceCommandID,
		"old-qa-action", orchestration.ActionDispatch, repository, 101, "qa", 150, otherHead, "", "", "", "",
		40, oldLane, orchestration.IntentCompleted, oldStamp, oldStamp); err != nil {
		t.Fatal(err)
	}
	oldLeaseID := "old-qa-lease"
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO workflow_lane_leases(
		id,project_id,lane_key,intent_id,claim_id,lease_owner,lease_token,status,acquired_at,expires_at,released_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, oldLeaseID, fixture.project.ID, oldLane, oldIntentID, "old-qa-claim",
		"scheduler", "old-qa-token", orchestration.LeaseCompleted, oldStamp, stamp, oldStamp); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO browser_workers(
		worker_id,protocol_version,capabilities_json,created_at,updated_at
	) VALUES(?,?,?,?,?)`, "old-qa-worker", "m11-c3-test", "[]", oldStamp, oldStamp); err != nil {
		t.Fatal(err)
	}
	oldBindingID := "old-qa-binding"
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO browser_lane_bindings(
		binding_id,project_id,lane_key,worker_id,target_kind,target_origin,target_path,target_label,
		enabled,binding_version,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,1,1,?,?)`, oldBindingID, fixture.project.ID, oldLane, "old-qa-worker",
		browserbinding.TargetKindChatGPTConversation, "https://chatgpt.com", "/c/old-qa", "", oldStamp, oldStamp); err != nil {
		t.Fatal(err)
	}
	attachmentsJSON, err := json.Marshal(fixture.request.Attachments)
	if err != nil {
		t.Fatal(err)
	}
	oldRequestID := "old-qa-provision"
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO session_provision_requests(
		id,project_id,intent_id,lane_lease_id,lane_key,issue_number,role,expected_head,attachment_profile,
		attachments_json,bootstrap_text,session_policy,chatgpt_project_url,expected_binding_version,status,
		claim_id,claim_owner,claim_token,claim_expires_at,worker_id,tab_id,target_kind,target_origin,target_path,
		completion_reason,attachment_evidence_json,created_at,updated_at,completed_at,observed_chatgpt_url,
		bound_binding_id,bound_binding_version
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, oldRequestID, fixture.project.ID,
		oldIntentID, oldLeaseID, oldLane, 101, "qa", otherHead, fixture.request.AttachmentProfile,
		string(attachmentsJSON), fixture.request.BootstrapText, orchestration.SessionPolicyFreshPerIntent,
		finalProjectURL, 0, orchestration.ProvisionProvisioned, "", "", "", "", "old-qa-worker", 66,
		browserbinding.TargetKindChatGPTConversation, "https://chatgpt.com", "/c/old-qa", "exact_session_bound",
		string(attachmentsJSON), oldStamp, oldStamp, oldStamp,
		"https://chatgpt.com/g/g-project/repository/c/old-qa", oldBindingID, 1); err != nil {
		t.Fatal(err)
	}

	finalized, err := fixture.finalizer.Finalize(ctx, fixture.finalizeInput("https://chatgpt.com/g/g-project/repository/c/final-chat"))
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != orchestration.ProvisionProvisioned {
		t.Fatalf("current request = %+v", finalized)
	}
	oldRequest, err := fixture.service.Get(ctx, oldRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if oldRequest.Status != orchestration.ProvisionSuperseded || oldRequest.CompletionReason != "replaced_by_current_session" {
		t.Fatalf("old request = %+v", oldRequest)
	}
	oldBinding, err := fixture.bindings.Get(ctx, fixture.project.ID, oldLane)
	if err != nil {
		t.Fatal(err)
	}
	if oldBinding.Enabled || oldBinding.BindingVersion != 2 || oldBinding.Readiness != "disabled" {
		t.Fatalf("old binding = %+v", oldBinding)
	}
	currentBinding, err := fixture.bindings.Get(ctx, fixture.project.ID, fixture.request.LaneKey)
	if err != nil {
		t.Fatal(err)
	}
	if !currentBinding.Enabled || currentBinding.Readiness != "ready" || currentBinding.BindingID != finalized.BoundBindingID {
		t.Fatalf("current binding = %+v", currentBinding)
	}
}
