package workflow

import "testing"

func TestLeadBlockedWithoutOwnerEscalationDoesNotRedispatchLeadLane(t *testing.T) {
	head := fullHead("8")
	issue := issueWith(head, eventComment(3, testTime, map[string]any{
		"role": "lead", "status": "blocked", "decision": "needs_input",
	}))

	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]

	if state.Route.Action != "manual_attention" || state.Route.ReasonCode != "lead_blocked_requires_decision" {
		t.Fatalf("route = %#v", state.Route)
	}
	if state.Route.TargetRole != "" || state.Route.LaneKey != "" {
		t.Fatalf("Lead blocker repeated a worker lane: %#v", state.Route)
	}
	if state.Attention.Kind != AttentionBlocked {
		t.Fatalf("attention = %#v", state.Attention)
	}
	if state.ActiveBlocker == nil || state.ActiveBlocker.Role != "lead" {
		t.Fatalf("active blocker = %#v", state.ActiveBlocker)
	}
}

func TestImplementorAndQABlockedStillRouteLead(t *testing.T) {
	head := fullHead("9")
	for _, role := range []string{"implementor", "qa"} {
		t.Run(role, func(t *testing.T) {
			fields := map[string]any{"role": role, "status": "blocked"}
			if role == "qa" {
				fields["head"] = head
				fields["verdict"] = "inconclusive"
			}
			state := DeriveProject(projectSnapshot("acme", "service", 1, issueWith(head, eventComment(4, testTime, fields)))).WorkUnits[0]
			if state.Route.Action != "dispatch" || state.Route.TargetRole != "lead" || state.Route.ReasonCode != "lead_first_blocker" {
				t.Fatalf("route = %#v", state.Route)
			}
		})
	}
}
