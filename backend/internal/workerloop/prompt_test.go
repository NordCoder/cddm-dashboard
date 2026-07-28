package workerloop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

func TestBuildWorkflowCommandIsDeterministicAndSideEffectFree(t *testing.T) {
	_, project, store, _ := testService(t, ":memory:")
	resources, err := resourcepack.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	engine := NewCommandEngine(store, resources)
	generation := testGeneration(project.ID, "implementor", "")

	first, err := engine.BuildWorkflowCommand(project.ID, 140, generation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.BuildWorkflowCommand(project.ID, 140, generation)
	if err != nil {
		t.Fatal(err)
	}
	if first.Input.ID != second.Input.ID || first.Prompt != second.Prompt {
		t.Fatalf("prepared command is not deterministic: first=%s second=%s", first.Input.ID, second.Input.ID)
	}
	if commands, err := store.ListCommands(context.Background(), project.ID, 140); err != nil || len(commands) != 0 {
		t.Fatalf("rendering persisted commands: count=%d err=%v", len(commands), err)
	}
	for _, expected := range []string{
		"# Dashboard Command Header",
		"Command ID: " + first.Input.ID,
		"Resources: cddm-dashboard-resources/v1.0",
		"Base methodology: cddm-minimal/v2.0",
		"Result protocol: cddm-worker-result/v1",
		"## Versioned Role Resource",
		"## Bounded Context Pack",
		"## Terminal Publication Contract",
		"command_id` = `" + first.Input.ID,
	} {
		if !strings.Contains(first.Prompt, expected) {
			t.Fatalf("prompt does not contain %q\n%s", expected, first.Prompt)
		}
	}

	persisted, err := engine.PersistWorkflowCommand(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ID != first.Input.ID || persisted.Status != CommandDeliveryPending {
		t.Fatalf("persisted = %+v", persisted)
	}
	again, err := engine.PersistWorkflowCommand(context.Background(), second)
	if err != nil || again.ID != persisted.ID {
		t.Fatalf("idempotent persistence = %+v err=%v", again, err)
	}
}

func TestDeliveryPlanningAdapterDoesNotAuthorizeExecution(t *testing.T) {
	_, project, store, _ := testService(t, ":memory:")
	resources, err := resourcepack.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	generation := testGeneration(project.ID, "qa", headA)
	adapter := NewDeliveryPlanningAdapter(staticPlanningReader{generation: generation}, NewCommandEngine(store, resources))
	result, err := adapter.Get(context.Background(), project.ID, 140, generation.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil || !strings.Contains(result.Plan.Prompt, "# Dashboard Command Header") || !strings.Contains(result.Plan.Prompt, "# CDDM Dashboard QA Worker") {
		t.Fatalf("rendered plan = %+v", result.Plan)
	}
	commands, err := store.ListCommands(context.Background(), project.ID, 140)
	if err != nil || len(commands) != 0 {
		t.Fatalf("authorization adapter persisted commands: count=%d err=%v", len(commands), err)
	}
}

func TestDelayedDeliveryOutcomeCannotRegressTerminalExecution(t *testing.T) {
	_, project, store, _ := testService(t, ":memory:")
	resources, err := resourcepack.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	engine := NewCommandEngine(store, resources)
	prepared, err := engine.BuildWorkflowCommand(project.ID, 140, testGeneration(project.ID, "implementor", ""))
	if err != nil {
		t.Fatal(err)
	}
	command, err := engine.PersistWorkflowCommand(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RecordDeliveryOutcome(context.Background(), command.ID, delivery.StatusDelivered); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAwaitingResult)
	if _, err := store.SetCommandStatus(context.Background(), command.ID, CommandCompleted); err != nil {
		t.Fatal(err)
	}
	if err := engine.RecordDeliveryOutcome(context.Background(), command.ID, delivery.StatusDelivered); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandCompleted)
}

func TestRejectedBrowserAuthorizationCreatesNoWorkflowCommand(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	resources, err := resourcepack.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	generation := testGeneration(project.ID, "implementor", "")
	coordinator := NewDeliveryCoordinator(db, failingBrowserDelivery{}, staticPlanningReader{generation: generation}, NewCommandEngine(store, resources))
	if _, err := coordinator.Create(context.Background(), project.ID, 140, delivery.Confirmation{PlanID: generation.PlanID}); !errors.Is(err, delivery.ErrConflict) {
		t.Fatalf("Create error = %v", err)
	}
	commands, err := store.ListCommands(context.Background(), project.ID, 140)
	if err != nil || len(commands) != 0 {
		t.Fatalf("rejected browser authorization persisted commands: count=%d err=%v", len(commands), err)
	}
}

func testGeneration(projectID int64, role, expectedHead string) planning.GenerationResult {
	contextHash := "context-hash"
	return planning.GenerationResult{
		Status: planning.StatusApproved,
		Context: planning.PromptContext{
			Version: planning.PromptContextVersion,
			Repository: planning.RepositoryIdentity{ProjectID: projectID, Owner: "NordCoder", Repository: "misak-website", WorkflowMode: "pull_request"},
			Issue: planning.IssueIdentity{GitHubID: 1400, Number: 140, Title: "Pilot", Lifecycle: "implementation"},
			CurrentHead: expectedHead,
			Route: workflow.Route{Action: "dispatch", TargetRole: role, LaneKey: "nordcoder/misak-website#140:" + role, ExpectedHead: expectedHead},
			ExpectedEvent: "worker_result",
			Warnings:      []workflow.Warning{},
			Evidence:      []planning.Evidence{},
			ContextHash:   contextHash,
		},
		Plan: &planning.PromptPlan{
			Version: planning.PromptPlanVersion, Action: "dispatch", TargetRole: role,
			LaneKey: "nordcoder/misak-website#140:" + role, ExpectedHead: expectedHead,
			ExpectedEvent: "worker_result", Prompt: "Perform the bounded planned action.",
			Source: planning.SourceMetadata{Kind: planning.SourceTemplateFallback, Mode: planning.ModeFallback, ContextHash: contextHash},
		},
		PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, ContextHash: contextHash, PlanHash: "plan-hash"},
		PlanID:         77,
	}
}

type staticPlanningReader struct {
	generation planning.GenerationResult
}

func (s staticPlanningReader) Get(context.Context, int64, int, int64) (planning.GenerationResult, error) {
	return s.generation, nil
}

func (s staticPlanningReader) ContextSummary(context.Context, int64, int) (planning.ContextSummary, error) {
	return planning.ContextSummary{ContextHash: s.generation.Context.ContextHash, CurrentHead: s.generation.Context.CurrentHead, Route: s.generation.Context.Route}, nil
}

type failingBrowserDelivery struct{}

func (failingBrowserDelivery) Create(context.Context, int64, int, delivery.Confirmation) (delivery.Command, error) {
	return delivery.Command{}, delivery.ErrConflict
}
func (failingBrowserDelivery) List(context.Context, int64, int) ([]delivery.Command, error) {
	return nil, nil
}
func (failingBrowserDelivery) ClaimNext(context.Context, delivery.ClaimRequest) (*delivery.Execution, error) {
	return nil, nil
}
func (failingBrowserDelivery) Complete(context.Context, delivery.Completion) (delivery.Command, error) {
	return delivery.Command{}, nil
}
func (failingBrowserDelivery) Reconcile(context.Context) error { return nil }
