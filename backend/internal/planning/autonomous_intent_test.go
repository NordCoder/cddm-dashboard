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
	mergePrompt := autonomousPrompt(contextValue, "merge_candidate")
	for _, fragment := range []string{
		"serialized Lead merge worker", "expected-Head protection", "do not retry an ambiguous merge", "cddm-worker-result/v2",
	} {
		if !strings.Contains(mergePrompt, fragment) {
			t.Fatalf("merge prompt missing %q: %s", fragment, mergePrompt)
		}
	}
	planningPrompt := autonomousPrompt(contextValue, "plan_next_wave")
	for _, fragment := range []string{"previous Wave", "Project Control Issue", "exactly one bounded `actions_ready` batch"} {
		if !strings.Contains(planningPrompt, fragment) {
			t.Fatalf("planning prompt missing %q: %s", fragment, planningPrompt)
		}
	}
}
