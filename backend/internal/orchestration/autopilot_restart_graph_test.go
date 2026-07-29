package orchestration_test

import (
	"database/sql"
	"testing"
)

type restartIdentityGraph struct {
	Project          []string
	Waves            []string
	WaveIssues       []string
	Intents          []string
	Leases           []string
	Provisioning     []string
	Workers          []string
	Bindings         []string
	Materializations []string
	WorkflowCommands []string
	DeliveryCommands []string
	DeliveryLinks    []string
	Results          []string
}

func captureRestartIdentityGraph(t *testing.T, db *sql.DB, projectID int64) restartIdentityGraph {
	t.Helper()
	return restartIdentityGraph{
		Project:          identityRows(t, db, `SELECT id||'|'||owner||'|'||repository||'|'||sync_status FROM projects WHERE id=? ORDER BY id`, projectID),
		Waves:            identityRows(t, db, `SELECT project_id||'|'||wave_id||'|'||control_issue_number||'|'||source_command_id||'|'||status FROM workflow_waves WHERE project_id=? ORDER BY wave_id`, projectID),
		WaveIssues:       identityRows(t, db, `SELECT project_id||'|'||wave_id||'|'||issue_number||'|'||position FROM workflow_wave_issues WHERE project_id=? ORDER BY wave_id,position`, projectID),
		Intents:          identityRows(t, db, `SELECT id||'|'||project_id||'|'||source_result_comment_id||'|'||source_command_id||'|'||action_id||'|'||action_type||'|'||repository||'|'||issue_number||'|'||role||'|'||pr_number||'|'||expected_head||'|'||expected_previous_head||'|'||wave_id||'|'||priority||'|'||lane_key||'|'||status FROM workflow_intents WHERE project_id=? ORDER BY id`, projectID),
		Leases:           identityRows(t, db, `SELECT id||'|'||project_id||'|'||lane_key||'|'||intent_id||'|'||claim_id||'|'||lease_owner||'|'||lease_token||'|'||status FROM workflow_lane_leases WHERE project_id=? ORDER BY id`, projectID),
		Provisioning:     identityRows(t, db, `SELECT id||'|'||project_id||'|'||intent_id||'|'||lane_lease_id||'|'||lane_key||'|'||issue_number||'|'||role||'|'||expected_head||'|'||attachment_profile||'|'||session_policy||'|'||chatgpt_project_url||'|'||expected_binding_version||'|'||status||'|'||claim_id||'|'||claim_owner||'|'||claim_token||'|'||worker_id||'|'||tab_id||'|'||target_kind||'|'||target_origin||'|'||target_path||'|'||observed_chatgpt_url||'|'||bound_binding_id||'|'||bound_binding_version FROM session_provision_requests WHERE project_id=? ORDER BY id`, projectID),
		Workers:          identityRows(t, db, `SELECT worker_id||'|'||protocol_version||'|'||capabilities_json FROM browser_workers ORDER BY worker_id`),
		Bindings:         identityRows(t, db, `SELECT binding_id||'|'||project_id||'|'||lane_key||'|'||worker_id||'|'||target_kind||'|'||target_origin||'|'||target_path||'|'||enabled||'|'||binding_version FROM browser_lane_bindings WHERE project_id=? ORDER BY binding_id`, projectID),
		Materializations: identityRows(t, db, `SELECT id||'|'||project_id||'|'||intent_id||'|'||lease_id||'|'||provision_request_id||'|'||scheduler_lane_key||'|'||delivery_lane_key||'|'||plan_id||'|'||status||'|'||workflow_command_id||'|'||delivery_command_id||'|'||context_hash||'|'||prompt_hash FROM autonomous_command_materializations WHERE project_id=? ORDER BY id`, projectID),
		WorkflowCommands: identityRows(t, db, `SELECT id||'|'||project_id||'|'||issue_number||'|'||identity_key||'|'||role||'|'||action||'|'||resource_profile||'|'||context_hash||'|'||expected_head||'|'||status FROM workflow_commands WHERE project_id=? ORDER BY id`, projectID),
		DeliveryCommands: identityRows(t, db, `SELECT id||'|'||project_id||'|'||issue_number||'|'||plan_id||'|'||idempotency_key||'|'||plan_hash||'|'||context_hash||'|'||prompt_hash||'|'||action||'|'||target_role||'|'||lane_key||'|'||expected_head||'|'||binding_id||'|'||binding_version||'|'||worker_id||'|'||worker_session_id||'|'||target_kind||'|'||target_ref||'|'||authority_kind||'|'||authority_ref||'|'||status||'|'||claim_id||'|'||claim_request_id||'|'||outcome_reason||'|'||outcome_evidence FROM delivery_commands WHERE project_id=? ORDER BY id`, projectID),
		DeliveryLinks:    identityRows(t, db, `SELECT l.workflow_command_id||'|'||l.delivery_command_id FROM workflow_delivery_links l JOIN workflow_commands w ON w.id=l.workflow_command_id WHERE w.project_id=? ORDER BY l.workflow_command_id,l.delivery_command_id`, projectID),
		Results:          identityRows(t, db, `SELECT project_id||'|'||github_comment_id||'|'||issue_number||'|'||asserted_command_id||'|'||role||'|'||result||'|'||payload_hash||'|'||validation_status||'|'||validation_reason FROM workflow_results WHERE project_id=? ORDER BY github_comment_id`, projectID),
	}
}

func assertRestartFixtureCardinality(t *testing.T, db *sql.DB, projectID int64) {
	t.Helper()
	checks := []struct {
		name  string
		query string
		want  int
	}{
		{name: "Wave", query: `SELECT COUNT(*) FROM workflow_waves WHERE project_id=?`, want: 1},
		{name: "Wave Issue", query: `SELECT COUNT(*) FROM workflow_wave_issues WHERE project_id=?`, want: 4},
		{name: "Intent", query: `SELECT COUNT(*) FROM workflow_intents WHERE project_id=? AND id LIKE 'intent-soak-%'`, want: 5},
		{name: "lease", query: `SELECT COUNT(*) FROM workflow_lane_leases WHERE project_id=?`, want: 4},
		{name: "provisioning", query: `SELECT COUNT(*) FROM session_provision_requests WHERE project_id=?`, want: 4},
		{name: "binding", query: `SELECT COUNT(*) FROM browser_lane_bindings WHERE project_id=?`, want: 3},
		{name: "materialization", query: `SELECT COUNT(*) FROM autonomous_command_materializations WHERE project_id=?`, want: 2},
		{name: "Workflow Command", query: `SELECT COUNT(*) FROM workflow_commands WHERE project_id=? AND id LIKE 'cmd-soak-%'`, want: 2},
		{name: "delivery command", query: `SELECT COUNT(*) FROM delivery_commands WHERE project_id=? AND id LIKE 'delivery-soak-%'`, want: 2},
		{name: "result evidence", query: `SELECT COUNT(*) FROM workflow_results WHERE project_id=? AND github_comment_id=99002`, want: 1},
	}
	for _, check := range checks {
		var got int
		if err := db.QueryRow(check.query, projectID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != check.want {
			t.Fatalf("%s cardinality = %d, want %d", check.name, got, check.want)
		}
	}
}

func identityRows(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}
