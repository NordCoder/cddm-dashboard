package orchestration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

func TestTypedPlanningAdapterBindsPriorWaveAndControlIssue(t *testing.T) {
	ctx := context.Background()
	db, project, store, scheduler, snapshot, sourceCommandID := schedulerFixture(t, ":memory:", 2, 2, 2)
	intent := schedulerIntent(
		project.ID, sourceCommandID, "typed-next-wave", orchestration.ActionPlanNextWave,
		90, "lead", 100, fmt.Sprintf("project:%d:lead", project.ID),
	)
	intent.WaveID = "wave-completed"
	createSchedulerIntents(t, store, project.ID, sourceCommandID, []orchestration.IntentInput{intent})
	decision, err := scheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "typed-next-wave-claim", LeaseOwner: "dashboard-autopilot",
		LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || !decision.Claimed {
		t.Fatalf("claim next-Wave Intent = %+v err=%v", decision, err)
	}

	planner := &nextWavePlannerRecorder{}
	adapter, err := orchestration.NewTypedPlanningAdapter(store, supervisor.NewStore(db), planner)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := adapter.GenerateAutonomousIntent(ctx, project.ID, 90, orchestration.ActionPlanNextWave, "lead", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(planner.details) != 1 {
		t.Fatalf("detail calls = %d", len(planner.details))
	}
	detail := planner.details[0]
	if detail.Action != orchestration.ActionPlanNextWave || detail.Repository != "NordCoder/app" || detail.Role != "lead" || detail.ExpectedHead != "" || detail.WaveID != "wave-completed" || detail.ControlIssue != 90 {
		t.Fatalf("next-Wave detail = %+v", detail)
	}
	if _, err := adapter.Get(ctx, project.ID, 90, generated.PlanID); err != nil {
		t.Fatalf("restart Get rejected next-Wave plan: %v", err)
	}
}

type nextWavePlannerRecorder struct {
	result  planning.GenerationResult
	details []planning.AutonomousIntentDetail
}

func (p *nextWavePlannerRecorder) GenerateAutonomousIntent(_ context.Context, projectID int64, issueNumber int, _ string, role, head string, supplied ...planning.AutonomousIntentDetail) (planning.GenerationResult, error) {
	if len(supplied) != 1 {
		return planning.GenerationResult{}, fmt.Errorf("one detail required")
	}
	p.details = append(p.details, supplied[0])
	encoded, err := json.Marshal(supplied[0])
	if err != nil {
		return planning.GenerationResult{}, err
	}
	lane := fmt.Sprintf("nordcoder/app#%d:%s", issueNumber, role)
	p.result = planning.GenerationResult{
		Status: planning.StatusFallback, PlanID: 88, CreatedAt: time.Now().UTC(),
		Context: planning.PromptContext{
			Version: planning.PromptContextVersion,
			Repository: planning.RepositoryIdentity{
				ProjectID: projectID, Owner: "NordCoder", Repository: "app", WorkflowMode: "pull_request",
			},
			Issue: planning.IssueIdentity{Number: issueNumber, Title: "Control", Lifecycle: "ready"},
			CurrentHead: head, ContextHash: "next-wave-context",
			Route: workflow.Route{Action: "dispatch", TargetRole: role, LaneKey: lane, ExpectedHead: head},
		},
		Plan: &planning.PromptPlan{
			Version: planning.PromptPlanVersion, Action: "dispatch", TargetRole: role, LaneKey: lane,
			ExpectedHead: head, Prompt: "next-Wave prompt",
			Extensions: map[string]json.RawMessage{planning.AutonomousIntentExtension: encoded},
		},
		PolicyDecision: planning.PolicyDecision{
			Status: planning.StatusApproved, ContextHash: "next-wave-context", PlanHash: "next-wave-plan-hash",
		},
	}
	return p.result, nil
}

func (p *nextWavePlannerRecorder) Get(context.Context, int64, int, int64) (planning.GenerationResult, error) {
	return p.result, nil
}
