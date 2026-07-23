package workflow

import (
	"testing"
	"time"
)

func TestCorrelatedOwnerEscalationKeepsWorkerBlockerPending(t *testing.T) {
	head := fullHead("d")
	for _, status := range []string{"completed", "blocked"} {
		t.Run(status, func(t *testing.T) {
			issue := issueWith(head,
				eventComment(1, testTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"}),
				eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "lead", "status": status, "decision": "owner_required", "escalate_to": "owner", "resolves": 1}),
			)

			state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
			if state.ActiveBlocker == nil || state.ActiveBlocker.CommentID != 1 {
				t.Fatalf("active blocker = %#v, want worker blocker 1 pending", state.ActiveBlocker)
			}
			if state.Route.Action != "owner_attention" || state.Route.ReasonCode != "owner_required" {
				t.Fatalf("route = %#v", state.Route)
			}
			if state.Route.TargetRole != "" || state.Route.LaneKey != "" {
				t.Fatalf("owner escalation dispatched worker lane: %#v", state.Route)
			}
			if state.Attention.Kind != AttentionOwnerRequired {
				t.Fatalf("attention = %#v", state.Attention)
			}
			for _, item := range state.Warnings {
				if item.Code == "additional_unresolved_blocker" && item.CommentID == 2 {
					t.Fatalf("correlated escalation became a second blocker: %#v", state.Warnings)
				}
			}
		})
	}
}

func TestMismatchedOwnerEscalationCannotBypassActiveBlocker(t *testing.T) {
	head := fullHead("e")
	issue := issueWith(head,
		eventComment(1, testTime, map[string]any{"role": "implementor", "status": "blocked"}),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "lead", "status": "completed", "decision": "owner_required", "escalate_to": "owner", "resolves": 999}),
	)

	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.ActiveBlocker == nil || state.ActiveBlocker.CommentID != 1 {
		t.Fatalf("active blocker = %#v", state.ActiveBlocker)
	}
	if state.Route.Action == "owner_attention" || state.Route.ReasonCode != "unresolved_active_blocker" {
		t.Fatalf("route = %#v", state.Route)
	}
}
