package workflow

import (
	"testing"
	"time"
)

func TestQueuedBlockerRemainsActiveAfterEarlierResolution(t *testing.T) {
	head := fullHead("a")
	issue := issueWith(head,
		eventComment(1, testTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"}),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "implementor", "status": "blocked"}),
		eventComment(3, testTime.Add(2*time.Minute), map[string]any{"role": "lead", "status": "completed", "decision": "continue", "resume_role": "qa", "resolves": 1}),
	)

	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.ActiveBlocker == nil || state.ActiveBlocker.CommentID != 2 {
		t.Fatalf("active blocker = %#v, want queued blocker 2", state.ActiveBlocker)
	}
	if state.Route.Action != "manual_attention" || state.Route.ReasonCode != "unresolved_active_blocker" {
		t.Fatalf("route = %#v", state.Route)
	}

	issue.Comments = append(issue.Comments,
		eventComment(4, testTime.Add(3*time.Minute), map[string]any{"role": "lead", "status": "completed", "decision": "correct", "resume_role": "implementor", "resolves": 2}),
	)
	resolved := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if resolved.ActiveBlocker != nil || resolved.Route.Action != "dispatch" || resolved.Route.TargetRole != "implementor" {
		t.Fatalf("resolved blocker=%#v route=%#v", resolved.ActiveBlocker, resolved.Route)
	}
}

func TestLeadCannotResumeItsOwnLane(t *testing.T) {
	head := fullHead("b")
	issue := issueWith(head,
		eventComment(1, testTime, map[string]any{"role": "lead", "status": "completed", "decision": "continue", "resume_role": "lead"}),
	)

	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.Route.Action == "dispatch" || state.Route.ReasonCode != "invalid_lead_resume_role" {
		t.Fatalf("route = %#v", state.Route)
	}
}

func TestLeadSelfResumeCannotClearWorkerBlocker(t *testing.T) {
	head := fullHead("c")
	issue := issueWith(head,
		eventComment(1, testTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"}),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "lead", "status": "no_op", "decision": "continue", "resume_role": "lead", "resolves": 1}),
	)

	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.ActiveBlocker == nil || state.ActiveBlocker.CommentID != 1 {
		t.Fatalf("active blocker = %#v", state.ActiveBlocker)
	}
	if state.Route.Action != "manual_attention" || state.Route.ReasonCode != "unresolved_active_blocker" {
		t.Fatalf("route = %#v", state.Route)
	}
	assertWarning(t, state.Warnings, "non_actionable_blocker_resolution")
}
