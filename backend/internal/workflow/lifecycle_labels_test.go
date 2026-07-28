package workflow

import (
	"testing"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func TestNormalizeLifecycleLabelAcceptsStatusNamespace(t *testing.T) {
	tests := map[string]string{
		"status:ready":          "ready",
		" STATUS: Ready ":       "ready",
		"status:ready-for-work": "ready",
		"status:in progress":    "implementation",
		"status:qa":             "qa",
		"status:blocked":        "blocked",
		"status:done":           "terminal",
		"ready":                 "ready",
		"status_ready":          "ready",
	}
	for label, expected := range tests {
		t.Run(label, func(t *testing.T) {
			if actual := normalizeLifecycleLabel(label); actual != expected {
				t.Fatalf("normalizeLifecycleLabel(%q) = %q, want %q", label, actual, expected)
			}
		})
	}
}

func TestDeriveLifecycleDeduplicatesEquivalentStatusLabels(t *testing.T) {
	labels := []supervisor.Label{
		{Name: "ready"},
		{Name: "status:ready"},
		{Name: "STATUS: READY"},
	}
	lifecycle, warnings := deriveLifecycle(labels, nil)
	if lifecycle != "ready" || len(warnings) != 0 {
		t.Fatalf("lifecycle = %q warnings = %#v", lifecycle, warnings)
	}
}

func TestDeriveLifecyclePreservesUnknownAndConflictSemantics(t *testing.T) {
	unknown, unknownWarnings := deriveLifecycle([]supervisor.Label{{Name: "status:future"}}, nil)
	if unknown != "unknown" {
		t.Fatalf("unknown lifecycle = %q", unknown)
	}
	assertWarning(t, unknownWarnings, "missing_lifecycle_label")

	conflicting, conflictWarnings := deriveLifecycle([]supervisor.Label{
		{Name: "status:ready"},
		{Name: "status:blocked"},
	}, nil)
	if conflicting != "unknown" {
		t.Fatalf("conflicting lifecycle = %q", conflicting)
	}
	assertWarning(t, conflictWarnings, "ambiguous_lifecycle_label")
}

func TestStatusReadyLabelDrivesNormalInitialRoute(t *testing.T) {
	head := fullHead("8")
	issue := issueWith(head)
	issue.Labels = []supervisor.Label{{Name: "status:ready"}}

	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.Lifecycle != "ready" {
		t.Fatalf("lifecycle = %q", state.Lifecycle)
	}
	if state.Route.Action != "dispatch" || state.Route.TargetRole != "implementor" || state.Route.ReasonCode != "initial_implementation" {
		t.Fatalf("route = %#v", state.Route)
	}
	for _, warning := range state.Warnings {
		if warning.Code == "missing_lifecycle_label" {
			t.Fatalf("status:ready produced a missing lifecycle warning: %#v", state.Warnings)
		}
	}
}
