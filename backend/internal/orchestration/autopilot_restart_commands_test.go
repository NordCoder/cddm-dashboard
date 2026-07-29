package orchestration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

type soakCommandChain struct {
	IntentID          string
	LeaseID           string
	MaterializationID string
	WorkflowID        string
	DeliveryID        string
	Confirmation      delivery.Confirmation
}

type restartAutonomousPlanner struct {
	results map[int]planning.GenerationResult
}

func (p *restartAutonomousPlanner) GenerateAutonomous(_ context.Context, _ int64, issueNumber int, role, expectedHead string) (planning.GenerationResult, error) {
	value, ok := p.results[issueNumber]
	if !ok || value.Plan == nil || value.Plan.TargetRole != role || value.Context.CurrentHead != expectedHead {
		return planning.GenerationResult{}, fmt.Errorf("restart plan unavailable for Issue #%d", issueNumber)
	}
	return value, nil
}

func (p *restartAutonomousPlanner) Get(_ context.Context, _ int64, issueNumber int, planID int64) (planning.GenerationResult, error) {
	value, ok := p.results[issueNumber]
	if !ok || value.PlanID != planID {
		return planning.GenerationResult{}, fmt.Errorf("restart plan %d unavailable for Issue #%d", planID, issueNumber)
	}
	return value, nil
}

func (p *restartAutonomousPlanner) ContextSummary(_ context.Context, _ int64, issueNumber int) (planning.ContextSummary, error) {
	value, ok := p.results[issueNumber]
	if !ok {
		return planning.ContextSummary{}, fmt.Errorf("restart context unavailable for Issue #%d", issueNumber)
	}
	return planning.ContextSummary{
		Version: planning.PromptContextVersion, ContextHash: value.Context.ContextHash,
		Repository: value.Context.Repository, Issue: value.Context.Issue, CurrentHead: value.Context.CurrentHead,
		Route: value.Context.Route, ExpectedEvent: value.Context.ExpectedEvent,
	}, nil
}

func restartGeneration(projectID int64, issueNumber int, role, expectedHead string, planID int64) planning.GenerationResult {
	lane := fmt.Sprintf("NordCoder/app#%d:%s", issueNumber, role)
	contextHash := fmt.Sprintf("restart-context-%d", issueNumber)
	planHash := fmt.Sprintf("restart-plan-%d", issueNumber)
	return planning.GenerationResult{
		Status: planning.StatusFallback, PlanID: planID, CreatedAt: time.Now().UTC(),
		Context: planning.PromptContext{
			Version: planning.PromptContextVersion,
			Repository: planning.RepositoryIdentity{ProjectID: projectID, Owner: "NordCoder", Repository: "app", WorkflowMode: "pull_request"},
			Issue: planning.IssueIdentity{Number: issueNumber, Title: fmt.Sprintf("Restart Issue %d", issueNumber), Lifecycle: "implementation"},
			CurrentHead: expectedHead, ContextHash: contextHash,
			Route: workflow.Route{Action: "dispatch", TargetRole: role, LaneKey: lane, ExpectedHead: expectedHead},
			ExpectedEvent: "worker_result",
		},
		Plan: &planning.PromptPlan{
			Version: planning.PromptPlanVersion, Action: "dispatch", TargetRole: role, LaneKey: lane,
			ExpectedHead: expectedHead, ExpectedEvent: "worker_result", Prompt: fmt.Sprintf("bounded restart prompt for Issue %d", issueNumber),
			Source: planning.SourceMetadata{Kind: planning.SourceTemplateFallback, Runtime: "restart_fixture", Mode: planning.ModeFallback, ContextHash: contextHash},
		},
		PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, ContextHash: contextHash, PlanHash: planHash, DecidedAt: time.Now().UTC()},
	}
}

func restartProductionRuntime(t *testing.T, db *sql.DB, store *orchestration.Store, scheduler *orchestration.Scheduler, provisions *orchestration.ProvisioningService, bindings *browserbinding.Service, plans map[int]planning.GenerationResult) (*orchestration.AutopilotEngine, *workerloop.DeliveryCoordinator) {
	t.Helper()
	resources, err := resourcepack.Load(resourcepack.V2Profile)
	if err != nil {
		t.Fatal(err)
	}
	planner := &restartAutonomousPlanner{results: plans}
	commands := workerloop.NewCommandEngine(workerloop.NewStore(db), resources)
	planningAdapter := workerloop.NewDeliveryPlanningAdapter(planner, commands)
	baseResolver := delivery.NewBrowserBindingResolver(bindings)
	autonomousResolver := orchestration.NewDeliveryBindingResolver(db, baseResolver)
	browserDelivery := delivery.New(db, planningAdapter, autonomousResolver, delivery.Config{Enabled: true, PendingTTL: time.Hour, ClaimTTL: time.Minute})
	coordinator := workerloop.NewDeliveryCoordinator(db, browserDelivery, planner, commands)
	engine, err := orchestration.NewAutopilotEngine(store, scheduler, provisions, planner, coordinator, baseResolver, supervisor.NewStore(db))
	if err != nil {
		t.Fatal(err)
	}
	return engine, coordinator
}

func readSoakCommandChain(t *testing.T, db *sql.DB, projectID int64, intentID string) soakCommandChain {
	t.Helper()
	var value soakCommandChain
	value.IntentID = intentID
	err := db.QueryRow(`SELECT m.id,m.lease_id,m.workflow_command_id,m.delivery_command_id,
		d.plan_id,d.idempotency_key,d.plan_hash,d.context_hash,d.expected_head,d.lane_key,d.binding_id,d.binding_version,d.presence_token
		FROM autonomous_command_materializations m
		JOIN delivery_commands d ON d.id=m.delivery_command_id
		WHERE m.project_id=? AND m.intent_id=?`, projectID, intentID).Scan(
		&value.MaterializationID, &value.LeaseID, &value.WorkflowID, &value.DeliveryID,
		&value.Confirmation.PlanID, &value.Confirmation.IdempotencyKey, &value.Confirmation.ExpectedPlanHash,
		&value.Confirmation.ExpectedContextHash, &value.Confirmation.ExpectedHead, &value.Confirmation.ExpectedLaneKey,
		&value.Confirmation.ExpectedBindingID, &value.Confirmation.ExpectedBindingVer, &value.Confirmation.ExpectedPresenceToken,
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func completeSoakDelivery(t *testing.T, coordinator *workerloop.DeliveryCoordinator, request orchestration.ProvisionRequest, sessionID string) {
	t.Helper()
	execution, err := coordinator.ClaimNext(context.Background(), delivery.ClaimRequest{
		WorkerID: request.WorkerID, WorkerSessionID: sessionID, ClaimRequestID: "restart-send-" + request.IntentID,
	})
	if err != nil || execution == nil {
		t.Fatalf("claim restart delivery for %s = %+v err=%v", request.IntentID, execution, err)
	}
	if _, err := coordinator.Complete(context.Background(), delivery.Completion{
		CommandID: execution.Command.ID, ClaimID: execution.ClaimID, Outcome: delivery.StatusDelivered,
		Reason: "delivered", Evidence: request.ObservedChatGPTURL,
	}); err != nil {
		t.Fatal(err)
	}
}
