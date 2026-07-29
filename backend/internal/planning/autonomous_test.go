package planning

import (
	"context"
	"strings"
	"testing"
)

func TestGenerateAutonomousPersistsExactDeterministicRolePlan(t *testing.T) {
	service, store, _, project := newPlanningTestService(t, &fakePlanner{}, true, "autonomous-repo")
	head := strings.Repeat("a", 40)
	seedPlanningSnapshot(t, store, project.ID, 11, head, "Autonomous implementation")

	result, err := service.GenerateAutonomous(context.Background(), project.ID, 11, "implementor", head)
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID == 0 || result.Plan == nil || result.PolicyDecision.Status != StatusApproved {
		t.Fatalf("autonomous generation = %#v", result)
	}
	if result.Plan.Action != "dispatch" || result.Plan.TargetRole != "implementor" || result.Plan.ExpectedHead != head {
		t.Fatalf("autonomous plan identity = %#v", result.Plan)
	}
	if !strings.Contains(result.Plan.Prompt, "Do not infer authority from chat history") || !strings.Contains(result.Plan.Prompt, "cddm-worker-result/v2") {
		t.Fatalf("autonomous prompt contract missing: %q", result.Plan.Prompt)
	}
	if strings.Contains(result.Plan.Prompt, "supervisor:event") {
		t.Fatalf("autonomous prompt inherited legacy result contract: %q", result.Plan.Prompt)
	}

	stored, err := service.Get(context.Background(), project.ID, 11, result.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PolicyDecision.PlanHash != result.PolicyDecision.PlanHash || stored.Context.ContextHash != result.Context.ContextHash || stored.Plan.Prompt != result.Plan.Prompt {
		t.Fatalf("stored autonomous plan changed: %#v / %#v", stored, result)
	}
}

func TestGenerateAutonomousRejectsRoleAndHeadDrift(t *testing.T) {
	service, store, _, project := newPlanningTestService(t, &fakePlanner{}, true, "autonomous-drift")
	head := strings.Repeat("b", 40)
	seedPlanningSnapshot(t, store, project.ID, 11, head, "Autonomous drift")

	if _, err := service.GenerateAutonomous(context.Background(), project.ID, 11, "qa", head); err == nil {
		t.Fatal("autonomous generation accepted a non-current role")
	}
	if _, err := service.GenerateAutonomous(context.Background(), project.ID, 11, "implementor", strings.Repeat("c", 40)); err == nil {
		t.Fatal("autonomous generation accepted a non-current Head")
	}
}
