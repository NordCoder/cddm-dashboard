package planning

import (
	"strings"
	"testing"

	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

func TestAutonomousIntentMatchesOnlyCompatibleCurrentRoutes(t *testing.T) {
	head := strings.Repeat("a", 40)
	merge := PromptContext{
		CurrentHead: head,
		Route:       workflow.Route{Action: "dispatch", TargetRole: "lead", ExpectedHead: head},
	}
	if !autonomousIntentMatchesRoute(merge, "merge_candidate", "lead", head) {
		t.Fatal("exact Lead merge Intent rejected")
	}
	if autonomousIntentMatchesRoute(merge, "merge_candidate", "lead", strings.Repeat("b", 40)) {
		t.Fatal("stale merge Head accepted")
	}
	if autonomousIntentMatchesRoute(merge, "merge_candidate", "implementor", head) {
		t.Fatal("non-Lead merge Intent accepted")
	}

	nextWave := PromptContext{
		CurrentHead: "",
		Route:       workflow.Route{Action: "dispatch", TargetRole: "lead", ExpectedHead: ""},
	}
	if !autonomousIntentMatchesRoute(nextWave, "plan_next_wave", "lead", "") {
		t.Fatal("Lead next-Wave Intent rejected")
	}
	if autonomousIntentMatchesRoute(nextWave, "plan_next_wave", "qa", "") {
		t.Fatal("QA next-Wave Intent accepted")
	}
}

func TestTypedAutonomousPromptMakesMergeAndPlanningBoundariesExplicit(t *testing.T) {
	head := strings.Repeat("c", 40)
	contextValue := PromptContext{
		Repository:  RepositoryIdentity{Owner: "NordCoder", Repository: "app"},
		Issue:       IssueIdentity{Number: 101, Title: "Candidate", Lifecycle: "qa"},
		CurrentHead: head, ContextHash: "context-hash",
		Route: workflow.Route{Action: "dispatch", TargetRole: "lead", ExpectedHead: head, Reason: "qa approved"},
	}
	mergeDetail := AutonomousIntentDetail{
		Action: "merge_candidate", Repository: "NordCoder/app", Role: "lead", ExpectedHead: head,
		PRNumber: 150, ExpectedBaseRef: "main", WaveID: "wave-7",
	}
	mergePrompt := autonomousPrompt(contextValue, "merge_candidate", mergeDetail)
	for _, fragment := range []string{
		"serialized Lead merge worker", "expected-Head protection", "do not retry an ambiguous merge",
		"cddm-worker-result/v2", "Authorized PR: #150", "Expected base branch: main",
	} {
		if !strings.Contains(mergePrompt, fragment) {
			t.Fatalf("merge prompt missing %q: %s", fragment, mergePrompt)
		}
	}
	planningDetail := AutonomousIntentDetail{
		Action: "plan_next_wave", Repository: "NordCoder/app", Role: "lead",
		WaveID: "wave-7", ControlIssue: 101,
	}
	planningPrompt := autonomousPrompt(contextValue, "plan_next_wave", planningDetail)
	for _, fragment := range []string{
		"previous Wave", "Project Control Issue", "exactly one bounded `actions_ready` batch",
		"Completed Wave: wave-7", "Project Control Issue: #101",
	} {
		if !strings.Contains(planningPrompt, fragment) {
			t.Fatalf("planning prompt missing %q: %s", fragment, planningPrompt)
		}
	}
}

func TestNormalizeAutonomousIntentDetailRequiresConsequentialIdentity(t *testing.T) {
	head := strings.Repeat("d", 40)
	contextValue := PromptContext{
		Repository: RepositoryIdentity{Owner: "NordCoder", Repository: "app"},
		Issue:      IssueIdentity{Number: 90},
	}
	if _, err := normalizeAutonomousIntentDetail(contextValue, "merge_candidate", "lead", head, nil); err == nil {
		t.Fatal("merge detail without PR/base was accepted")
	}
	if _, err := normalizeAutonomousIntentDetail(contextValue, "plan_next_wave", "lead", "", []AutonomousIntentDetail{{
		Action: "plan_next_wave", Repository: "NordCoder/app", Role: "lead", WaveID: "wave-7", ControlIssue: 91,
	}}); err == nil {
		t.Fatal("next-Wave detail for a different Control Issue was accepted")
	}
}
