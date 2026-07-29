package orchestration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func TestAutonomousMergeDeliveryCannotBeClaimedBeforeDurableActivation(t *testing.T) {
	ctx := context.Background()
	db, project := testProject(t, ":memory:")
	store := orchestration.NewStore(db)
	sourceCommand, sourceResultID := seedLeadResult(t, db, project.ID)
	intentInput := orchestration.IntentInput{
		ID: "intent-activation-merge", ProjectID: project.ID, SourceResultCommentID: sourceResultID,
		SourceCommandID: sourceCommand.ID, ActionID: "activation-merge", ActionType: orchestration.ActionMerge,
		Repository: "NordCoder/app", IssueNumber: 101, Role: "lead", PRNumber: 150,
		ExpectedHead: testHead, Priority: 20, LaneKey: fmt.Sprintf("project:%d:lead", project.ID),
		Status: orchestration.IntentPending,
	}
	created, err := store.CreateBatch(ctx, nil, []orchestration.IntentInput{intentInput})
	if err != nil || len(created) != 1 {
		t.Fatalf("create Intent = %+v err=%v", created, err)
	}
	commands := workerloop.NewStore(db)
	command, err := commands.CreateCommand(ctx, workerloop.CreateCommandInput{
		ID: "cmd-activation-merge", ProjectID: project.ID, IssueNumber: 101,
		IdentityKey: "activation-merge", Role: "lead", Action: "dispatch",
		ResourceProfile: orchestration.ContinuousResourceProfile, ContextHash: "context",
		ExpectedHead: testHead, Status: workerloop.CommandDeliveryPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	deliveryID := "delivery-activation-merge"
	if _, err := db.ExecContext(ctx, `INSERT INTO delivery_commands(
		id,project_id,issue_number,plan_id,idempotency_key,confirmation_fingerprint,plan_hash,context_hash,
		prompt_hash,prompt_text,action,target_role,lane_key,expected_head,binding_id,binding_version,
		worker_id,worker_session_id,presence_token,target_kind,target_ref,status,created_at,expires_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		deliveryID, project.ID, 101, 1, "autopilot:"+created[0].ID, "fingerprint", "plan-hash", "context",
		"prompt-hash", "merge prompt", "dispatch", "lead", "nordcoder/app#101:lead", testHead,
		"binding", 1, "worker", "session", "presence", "chatgpt_conversation", "https://chatgpt.com/c/merge",
		"pending", now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_delivery_links(workflow_command_id,delivery_command_id,created_at) VALUES(?,?,?)`, command.ID, deliveryID, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var authorityKind, authorityRef string
	if err := db.QueryRowContext(ctx, `SELECT authority_kind,authority_ref FROM delivery_commands WHERE id=?`, deliveryID).Scan(&authorityKind, &authorityRef); err != nil {
		t.Fatal(err)
	}
	if authorityKind != "autonomous_intent" || authorityRef != created[0].ID {
		t.Fatalf("authority = %s/%s", authorityKind, authorityRef)
	}
	assertClaimIgnored(t, db, deliveryID, "without materialization")

	if _, err := db.ExecContext(ctx, `INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,delivery_lane_key,plan_id,status,
		workflow_command_id,delivery_command_id,context_hash,prompt_hash,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"autocmd-activation", project.ID, created[0].ID, "lease", "provision", created[0].LaneKey,
		"nordcoder/app#101:lead", 1, "materialized", command.ID, deliveryID, "context", "prompt-hash",
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	assertClaimIgnored(t, db, deliveryID, "without merge cycle")

	if _, err := db.ExecContext(ctx, `INSERT INTO merge_cycle_readbacks(
		id,project_id,intent_id,workflow_command_id,repository,issue_number,pr_number,approved_head,
		expected_base_ref,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,'pending',?,?)`,
		"merge-cycle-activation", project.ID, created[0].ID, command.ID, "NordCoder/app", 101, 150,
		testHead, "main", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	result, err := db.ExecContext(ctx, `UPDATE delivery_commands SET status='claimed' WHERE id=? AND status='pending'`, deliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		t.Fatalf("activated claim rows = %d, want 1", count)
	}
}

func assertClaimIgnored(t *testing.T, db *sql.DB, deliveryID, stage string) {
	t.Helper()
	result, err := db.ExecContext(context.Background(), `UPDATE delivery_commands SET status='claimed' WHERE id=? AND status='pending'`, deliveryID)
	if err != nil {
		t.Fatal(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("claim %s rows = %d, want 0", stage, count)
	}
}
