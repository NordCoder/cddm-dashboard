package orchestration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

func TestTypedPlanningAdapterBindsMergeIdentityAndRejectsCachedDrift(t *testing.T) {
	ctx := context.Background()
	db, project := testProject(t, ":memory:")
	store := orchestration.NewStore(db)
	if _, err := store.UpdateProjectProfile(ctx, orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		MaxActiveWorkUnits: 2, MaxParallelImplementors: 2, MaxParallelQA: 2,
	}); err != nil {
		t.Fatal(err)
	}
	sourceCommand, sourceResultID := seedLeadResult(t, db, project.ID)
	lane := fmt.Sprintf("project:%d:lead", project.ID)
	created, err := store.CreateBatch(ctx, nil, []orchestration.IntentInput{{
		ID: "intent-typed-plan", ProjectID: project.ID, SourceResultCommentID: sourceResultID,
		SourceCommandID: sourceCommand.ID, ActionID: "typed-merge", ActionType: orchestration.ActionMerge,
		Repository: "NordCoder/app", IssueNumber: 101, Role: "lead", PRNumber: 150,
		ExpectedHead: testHead, WaveID: "wave-typed", Priority: 20, LaneKey: lane,
		Status: orchestration.IntentPending,
	}})
	if err != nil || len(created) != 1 {
		t.Fatalf("create typed Intent = %+v err=%v", created, err)
	}
	now := time.Now().UTC()
	snapshot := supervisor.ProjectSnapshot{Project: project, Issues: []supervisor.Issue{
		{GitHubID: 9001, Number: 90, Title: "Control", State: "open", CreatedAt: now, UpdatedAt: now},
		{
			GitHubID: 9101, Number: 101, Title: "Candidate", State: "open", CreatedAt: now, UpdatedAt: now,
			PullRequests: []supervisor.PullRequest{{
				GitHubID: 9150, Number: 150, Title: "Candidate", State: "open", BaseRef: "main",
				HeadRef: "change", HeadSHA: testHead, UpdatedAt: now,
			}},
		},
	}}
	if err := supervisor.NewStore(db).ReplaceSnapshot(ctx, project.ID, supervisor.RepositorySnapshot{FetchedAt: now, Issues: snapshot.Issues}); err != nil {
		t.Fatal(err)
	}
	decision, err := orchestration.NewScheduler(store).ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "typed-plan-claim", LeaseOwner: "dashboard-autopilot",
		LeaseTTL: time.Hour, Snapshot: snapshot,
	})
	if err != nil || !decision.Claimed {
		t.Fatalf("claim typed Intent = %+v err=%v", decision, err)
	}

	planner := &typedPlannerRecorder{}
	adapter, err := orchestration.NewTypedPlanningAdapter(store, supervisor.NewStore(db), planner)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := adapter.GenerateAutonomousIntent(ctx, project.ID, 101, orchestration.ActionMerge, "lead", testHead)
	if err != nil {
		t.Fatal(err)
	}
	if len(planner.details) != 1 {
		t.Fatalf("detail calls = %d", len(planner.details))
	}
	detail := planner.details[0]
	if detail.Action != orchestration.ActionMerge || detail.Repository != "NordCoder/app" || detail.Role != "lead" || detail.ExpectedHead != testHead || detail.PRNumber != 150 || detail.ExpectedBaseRef != "main" || detail.WaveID != "wave-typed" {
		t.Fatalf("typed detail = %+v", detail)
	}
	if generated.Plan == nil || generated.Plan.Extensions[planning.AutonomousIntentExtension] == nil {
		t.Fatalf("generated plan = %+v", generated.Plan)
	}
	if _, err := adapter.Get(ctx, project.ID, 101, generated.PlanID); err != nil {
		t.Fatalf("restart Get rejected current plan: %v", err)
	}

	mutated := generated
	mutatedDetail := detail
	mutatedDetail.ExpectedBaseRef = "release"
	encoded, err := json.Marshal(mutatedDetail)
	if err != nil {
		t.Fatal(err)
	}
	mutated.Plan.Extensions[planning.AutonomousIntentExtension] = encoded
	planner.result = mutated
	if _, err := adapter.Get(ctx, project.ID, 101, generated.PlanID); !errors.Is(err, orchestration.ErrConflict) {
		t.Fatalf("mutated cached detail error = %v, want conflict", err)
	}
}

type typedPlannerRecorder struct {
	result  planning.GenerationResult
	details []planning.AutonomousIntentDetail
}

func (p *typedPlannerRecorder) GenerateAutonomousIntent(_ context.Context, projectID int64, issueNumber int, action, role, head string, supplied ...planning.AutonomousIntentDetail) (planning.GenerationResult, error) {
	if len(supplied) != 1 {
		return planning.GenerationResult{}, fmt.Errorf("one detail required")
	}
	p.details = append(p.details, supplied[0])
	encoded, err := json.Marshal(supplied[0])
	if err != nil {
		return planning.GenerationResult{}, err
	}
	lane := "nordcoder/app#101:lead"
	p.result = planning.GenerationResult{
		Status: planning.StatusFallback, PlanID: 77, CreatedAt: time.Now().UTC(),
		Context: planning.PromptContext{
			Version: planning.PromptContextVersion,
			Repository: planning.RepositoryIdentity{ProjectID: projectID, Owner: "NordCoder", Repository: "app", WorkflowMode: "pull_request"},
			Issue: planning.IssueIdentity{Number: issueNumber, Title: "Candidate", Lifecycle: "qa"},
			CurrentHead: head, ContextHash: "typed-context",
			Route: workflow.Route{Action: "dispatch", TargetRole: role, LaneKey: lane, ExpectedHead: head},
		},
		Plan: &planning.PromptPlan{
			Version: planning.PromptPlanVersion, Action: "dispatch", TargetRole: role, LaneKey: lane,
			ExpectedHead: head, Prompt: "typed prompt",
			Extensions: map[string]json.RawMessage{planning.AutonomousIntentExtension: encoded},
		},
		PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, ContextHash: "typed-context", PlanHash: "typed-plan-hash"},
	}
	return p.result, nil
}

func (p *typedPlannerRecorder) Get(context.Context, int64, int, int64) (planning.GenerationResult, error) {
	return p.result, nil
}
