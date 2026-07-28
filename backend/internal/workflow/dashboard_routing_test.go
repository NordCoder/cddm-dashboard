package workflow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func TestActiveDashboardCommandSuppressesDuplicateDispatch(t *testing.T) {
	head := fullHead("a")
	snapshot := projectSnapshot("acme", "service", 41, issueWith(head))
	for _, test := range []struct {
		status string
		code   string
		action string
	}{
		{status: "delivery_pending", code: "workflow_delivery_pending", action: "none"},
		{status: "awaiting_result", code: "awaiting_worker_result", action: "none"},
		{status: "ambiguous", code: "workflow_command_ambiguous", action: "manual_attention"},
		{status: "failed", code: "workflow_command_failed", action: "manual_attention"},
	} {
		t.Run(test.status, func(t *testing.T) {
			state := deriveProjectWithExternalState(snapshot, nil, map[int]ExternalCommand{
				6: {ID: "cmd-1", IssueNumber: 6, Role: "implementor", Action: "dispatch", ResourceProfile: "cddm-dashboard-resources/v1.0", ContextHash: "ctx", ExpectedHead: head, Status: test.status, CreatedAt: testTime},
			}).WorkUnits[0]
			if state.Route.Action != test.action || state.Route.ReasonCode != test.code {
				t.Fatalf("route = %#v", state.Route)
			}
			if test.action == "none" && state.Route.TargetRole != "" {
				t.Fatalf("active command exposed a duplicate target role: %#v", state.Route)
			}
		})
	}
}

func TestDashboardImplementorContinueDoesNotWaitForCI(t *testing.T) {
	head := fullHead("b")
	issue := issueWith(head, comment(101, testTime, "## Implementor Handoff\n\nMore bounded work is required."))
	issue.PullRequests[0].CI = supervisor.CISummary{HeadSHA: head, Status: "queued", Conclusion: "", UpdatedAt: testTime}
	external := map[int64]ExternalResult{101: dashboardResult("implementor", "continue", "", "", nil)}
	state := DeriveProjectWithExternal(projectSnapshot("acme", "service", 42, issue), external).WorkUnits[0]
	if state.Route.Action != "dispatch" || state.Route.TargetRole != "implementor" || state.Route.ReasonCode != "implementation_continue" {
		t.Fatalf("route = %#v", state.Route)
	}
}

func TestDashboardNoOpRequiresLeadReview(t *testing.T) {
	head := fullHead("c")
	issue := issueWith(head, comment(101, testTime, "## Implementor Handoff\n\nNo source change is required."))
	external := map[int64]ExternalResult{101: dashboardResult("implementor", "no_op", "", "", nil)}
	state := DeriveProjectWithExternal(projectSnapshot("acme", "service", 43, issue), external).WorkUnits[0]
	if state.Route.TargetRole != "lead" || state.Route.ReasonCode != "implementation_no_op" {
		t.Fatalf("route = %#v", state.Route)
	}
}

func TestDashboardQAChangesRequiredRoutesThroughLead(t *testing.T) {
	head := fullHead("d")
	issue := issueWith(head, comment(101, testTime, "## QA Verdict\n\nChanges are required."))
	external := map[int64]ExternalResult{101: dashboardResult("qa", "completed", head, "changes_required", map[string]any{"blocking_findings": 2, "cycle_escalation": "none"})}
	state := DeriveProjectWithExternal(projectSnapshot("acme", "service", 44, issue), external).WorkUnits[0]
	if state.Route.TargetRole != "lead" || state.Route.ReasonCode != "qa_changes_required" {
		t.Fatalf("route = %#v", state.Route)
	}
}

func TestDashboardQueuedCIRetainsCandidateThenRequestsFreshQA(t *testing.T) {
	head := fullHead("e")
	issue := issueWith(head, comment(101, testTime, "## QA Verdict\n\nExact-Candidate CI is queued."))
	external := map[int64]ExternalResult{101: dashboardResult("qa", "inconclusive", head, "inconclusive", map[string]any{
		"blocking_findings": 0,
		"blocker_type":      "process",
		"reason_code":       "exact_candidate_ci_queued",
	})}
	issue.PullRequests[0].CI = supervisor.CISummary{HeadSHA: head, Status: "queued", UpdatedAt: testTime}
	waiting := DeriveProjectWithExternal(projectSnapshot("acme", "service", 45, issue), external).WorkUnits[0]
	if waiting.Route.Action != "none" || waiting.Route.ReasonCode != "waiting_for_ci" || !containsString(waiting.Route.Guards, "same_candidate") || !containsString(waiting.Route.Guards, "no_correction_cycle") {
		t.Fatalf("waiting route = %#v", waiting.Route)
	}

	issue.PullRequests[0].CI = supervisor.CISummary{HeadSHA: head, Status: "completed", Conclusion: "success", UpdatedAt: testTime.Add(time.Minute)}
	ready := DeriveProjectWithExternal(projectSnapshot("acme", "service", 45, issue), external).WorkUnits[0]
	if ready.Route.Action != "dispatch" || ready.Route.TargetRole != "qa" || ready.Route.ReasonCode != "qa_retry_after_ci" || !containsString(ready.Route.Guards, "fresh_qa_session") {
		t.Fatalf("ready route = %#v", ready.Route)
	}
}

func TestDashboardReadyToMergeRunsVerifiedMergeGate(t *testing.T) {
	head := fullHead("f")
	issue := issueWith(head,
		comment(101, testTime, "## QA Verdict\n\nApproved."),
		comment(102, testTime.Add(time.Minute), "## Lead Decision\n\nReady to merge."),
	)
	issue.PullRequests[0].MergeableState = "clean"
	external := map[int64]ExternalResult{
		101: dashboardResult("qa", "completed", head, "approved", map[string]any{"blocking_findings": 0}),
		102: dashboardResult("lead", "completed", head, "", map[string]any{"result": "ready_to_merge", "pr": 7}),
	}
	external[102] = dashboardLeadResult("ready_to_merge", head)
	state := DeriveProjectWithExternal(projectSnapshot("acme", "service", 46, issue), external).WorkUnits[0]
	if state.Route.Action != "merge_gate" || state.Route.ReasonCode != "ready_to_merge_verification" || state.QAApprovedHead != head {
		t.Fatalf("state = %#v", state)
	}
}

func dashboardResult(role, status, head, verdict string, extra map[string]any) ExternalResult {
	extensions := map[string]json.RawMessage{"command_id": json.RawMessage(`"cmd-dashboard"`)}
	for key, value := range extra {
		encoded, _ := json.Marshal(value)
		extensions[key] = encoded
	}
	return ExternalResult{
		CommentID:      101,
		TransitionSafe: true,
		Event: &WorkerEvent{
			Version: 1, Event: "worker_result", Role: role, Status: status,
			Head: head, Verdict: verdict, Extensions: extensions,
		},
	}
}

func dashboardLeadResult(decision, head string) ExternalResult {
	return ExternalResult{
		CommentID: 102, TransitionSafe: true,
		Event: &WorkerEvent{
			Version: 1, Event: "worker_result", Role: "lead", Status: "completed",
			Head: head, Decision: decision,
			Extensions: map[string]json.RawMessage{
				"command_id": json.RawMessage(`"cmd-lead"`),
				"result":     json.RawMessage(`"ready_to_merge"`),
				"pr":         json.RawMessage(`7`),
			},
		},
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
