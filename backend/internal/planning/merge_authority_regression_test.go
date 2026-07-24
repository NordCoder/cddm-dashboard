package planning

import (
	"testing"
	"time"
)

func TestMergeAuthorityEquivalentWordingIsRejected(t *testing.T) {
	contextValue := policyContext("dispatch", "implementor", "nordcoder/cddm-dashboard#11:implementor")
	base := RenderFallback(contextValue)
	tests := []string{
		"Merge this PR.",
		"Merge the current pull request.",
		"Please merge that candidate.",
		"Merge it.",
		"Merge these changes.",
		"Merge the draft branch.",
	}
	for _, line := range tests {
		t.Run(line, func(t *testing.T) {
			plan := base
			plan.Guards = append([]string(nil), base.Guards...)
			plan.ProhibitedActions = append([]string(nil), base.ProhibitedActions...)
			plan.Prompt = line + "\n" + base.Prompt

			decision := ValidatePlan(contextValue, plan, contextValue, time.Now())
			if decision.Status != StatusRejected || !violationContains(decision.Violations, "forbidden_authority") {
				t.Fatalf("decision = %#v, want forbidden_authority for %q", decision, line)
			}
		})
	}
}

func TestMergePolicyAllowsDescriptiveAndExplicitlyProhibitedWording(t *testing.T) {
	contextValue := policyContext("dispatch", "implementor", "nordcoder/cddm-dashboard#11:implementor")
	base := RenderFallback(contextValue)
	tests := []string{
		"Report the merge status without changing GitHub state.",
		"Do not merge this PR.",
		"You must not merge the current pull request.",
		"Never merge these changes.",
	}
	for _, line := range tests {
		t.Run(line, func(t *testing.T) {
			plan := base
			plan.Guards = append([]string(nil), base.Guards...)
			plan.ProhibitedActions = append([]string(nil), base.ProhibitedActions...)
			plan.Prompt = line + "\n" + base.Prompt

			decision := ValidatePlan(contextValue, plan, contextValue, time.Now())
			if decision.Status != StatusApproved {
				t.Fatalf("safe merge wording %q was rejected: %#v", line, decision)
			}
		})
	}
}
