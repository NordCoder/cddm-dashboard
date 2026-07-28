package workerloop

import (
	"context"
	"testing"
	"time"
)

func TestAcceptedTerminalQARetiresCapturedBindingOnly(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-qa-retire", "qa", headA, CommandAwaitingResult)
	if _, err := store.SetCommandStatus(context.Background(), command.ID, CommandCompleted); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO browser_workers(worker_id,created_at,updated_at) VALUES('worker-qa',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO browser_lane_bindings(binding_id,project_id,lane_key,worker_id,target_kind,target_origin,target_path,enabled,binding_version,created_at,updated_at) VALUES('binding-qa',?,?, 'worker-qa','chatgpt_conversation','https://chatgpt.com','/c/qa',1,1,?,?)`, project.ID, "nordcoder/misak-website#140:qa", now, now); err != nil {
		t.Fatal(err)
	}
	insertTestDeliveryCommand(t, db, project.ID, 140, "delivery-qa", "delivered")
	if _, err := db.Exec(`UPDATE delivery_commands SET binding_id='binding-qa',binding_version=1,target_role='qa' WHERE id='delivery-qa'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO workflow_delivery_links(workflow_command_id,delivery_command_id,created_at) VALUES(?,?,?)`, command.ID, "delivery-qa", now); err != nil {
		t.Fatal(err)
	}
	accepted := time.Now().UTC()
	if err := store.UpsertResult(context.Background(), Result{ProjectID: project.ID, GitHubCommentID: 2001, IssueNumber: 140, CommandID: command.ID, Role: "qa", Result: "approved", Payload: []byte(`{}`), PayloadHash: "hash", ValidationStatus: ValidationAccepted, AcceptedAt: &accepted, ObservedAt: accepted}); err != nil {
		t.Fatal(err)
	}
	retirer := NewQABindingRetirer(db)
	if err := retirer.RetireAcceptedTerminalQA(context.Background(), project.ID); err != nil {
		t.Fatal(err)
	}
	var enabled int
	var version int64
	if err := db.QueryRow(`SELECT enabled,binding_version FROM browser_lane_bindings WHERE binding_id='binding-qa'`).Scan(&enabled, &version); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 || version != 2 {
		t.Fatalf("binding enabled=%d version=%d", enabled, version)
	}
}

func TestQARetirementDoesNotDisableNewerReplacementVersion(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-qa-stale-binding", "qa", headA, CommandAwaitingResult)
	if _, err := store.SetCommandStatus(context.Background(), command.ID, CommandCompleted); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO browser_workers(worker_id,created_at,updated_at) VALUES('worker-qa-2',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO browser_lane_bindings(binding_id,project_id,lane_key,worker_id,target_kind,target_origin,target_path,enabled,binding_version,created_at,updated_at) VALUES('binding-qa-2',?,?, 'worker-qa-2','chatgpt_conversation','https://chatgpt.com','/c/qa-new',1,2,?,?)`, project.ID, "nordcoder/misak-website#140:qa", now, now); err != nil {
		t.Fatal(err)
	}
	insertTestDeliveryCommand(t, db, project.ID, 140, "delivery-qa-2", "delivered")
	if _, err := db.Exec(`UPDATE delivery_commands SET binding_id='binding-qa-2',binding_version=1,target_role='qa' WHERE id='delivery-qa-2'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO workflow_delivery_links(workflow_command_id,delivery_command_id,created_at) VALUES(?,?,?)`, command.ID, "delivery-qa-2", now); err != nil {
		t.Fatal(err)
	}
	accepted := time.Now().UTC()
	if err := store.UpsertResult(context.Background(), Result{ProjectID: project.ID, GitHubCommentID: 2002, IssueNumber: 140, CommandID: command.ID, Role: "qa", Result: "approved", Payload: []byte(`{}`), PayloadHash: "hash", ValidationStatus: ValidationAccepted, AcceptedAt: &accepted, ObservedAt: accepted}); err != nil {
		t.Fatal(err)
	}
	if err := NewQABindingRetirer(db).RetireAcceptedTerminalQA(context.Background(), project.ID); err != nil {
		t.Fatal(err)
	}
	var enabled int
	var version int64
	if err := db.QueryRow(`SELECT enabled,binding_version FROM browser_lane_bindings WHERE binding_id='binding-qa-2'`).Scan(&enabled, &version); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 || version != 2 {
		t.Fatalf("replacement binding enabled=%d version=%d", enabled, version)
	}
}
