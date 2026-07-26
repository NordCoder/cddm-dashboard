package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

type fakePlans struct {
	result  planning.GenerationResult
	summary planning.ContextSummary
}

func (f fakePlans) Get(context.Context, int64, int, int64) (planning.GenerationResult, error) {
	return f.result, nil
}
func (f fakePlans) ContextSummary(context.Context, int64, int) (planning.ContextSummary, error) {
	return f.summary, nil
}

type fakeBindings struct {
	value BindingSnapshot
	err   error
}

func (f fakeBindings) Resolve(context.Context, int64, string) (BindingSnapshot, error) {
	return f.value, f.err
}

func TestDeliveryCreateClaimCompletionAndDeadline(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	project, err := supervisor.NewStore(db).CreateProject(ctx, supervisor.CreateProjectInput{Owner: "acme", Repository: "service", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	planHash := "plan-hash"
	lane := "acme/service#7:implementor"
	plan := &planning.PromptPlan{Action: "dispatch", TargetRole: "implementor", LaneKey: lane, ExpectedHead: "head", Prompt: "implement exactly this", Source: planning.SourceMetadata{ContextHash: "context"}}
	plans := fakePlans{result: planning.GenerationResult{Status: planning.StatusApproved, Plan: plan, PlanID: 4, PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, PlanHash: planHash}}, summary: planning.ContextSummary{ContextHash: "context", CurrentHead: "head", Route: workflow.Route{Action: "dispatch", TargetRole: "implementor", LaneKey: lane}}}
	binding := fakeBindings{value: BindingSnapshot{LaneKey: lane, BindingID: "binding", BindingVersion: 1, WorkerID: "worker", WorkerSessionID: "session", TargetKind: "chatgpt_conversation", TargetRef: "conversation/one", Ready: true, PresenceToken: "presence"}}
	service := New(db, plans, binding, Config{Enabled: true, PendingTTL: 5 * time.Minute, ClaimTTL: time.Minute, Now: func() time.Time { return now }})
	confirmation := Confirmation{PlanID: 4, IdempotencyKey: "confirm-1", ExpectedPlanHash: planHash, ExpectedContextHash: "context", ExpectedHead: "head", ExpectedLaneKey: lane, ExpectedBindingID: "binding", ExpectedBindingVer: 1, ExpectedPresenceToken: "presence"}
	command, err := service.Create(ctx, project.ID, 7, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if command.Status != StatusPending || command.Prompt != "implement exactly this" {
		t.Fatalf("command = %#v", command)
	}
	again, err := service.Create(ctx, project.ID, 7, confirmation)
	if err != nil || again.ID != command.ID {
		t.Fatalf("idempotent create = %#v, %v", again, err)
	}
	confirmation.ExpectedHead = "other"
	if _, err := service.Create(ctx, project.ID, 7, confirmation); !errors.Is(err, ErrConflict) {
		t.Fatalf("different key reuse error = %v", err)
	}
	claim, err := service.ClaimNext(ctx, ClaimRequest{WorkerID: "worker", WorkerSessionID: "session", ClaimRequestID: "request-1"})
	if err != nil || claim == nil {
		t.Fatalf("claim = %#v, %v", claim, err)
	}
	if claim.ClaimID == "" || claim.Prompt != "implement exactly this" {
		t.Fatalf("claim payload = %#v", claim)
	}
	repeat, err := service.ClaimNext(ctx, ClaimRequest{WorkerID: "worker", WorkerSessionID: "session", ClaimRequestID: "request-1"})
	if err != nil || repeat.ClaimID != claim.ClaimID {
		t.Fatalf("idempotent claim = %#v, %v", repeat, err)
	}
	completed, err := service.Complete(ctx, Completion{CommandID: command.ID, ClaimID: claim.ClaimID, Outcome: StatusDelivered})
	if err != nil || completed.Status != StatusDelivered {
		t.Fatalf("complete = %#v, %v", completed, err)
	}
	repeated, err := service.Complete(ctx, Completion{CommandID: command.ID, ClaimID: claim.ClaimID, Outcome: StatusDelivered})
	if err != nil || repeated.ID != command.ID || repeated.Status != StatusDelivered {
		t.Fatalf("identical completion = %#v, %v", repeated, err)
	}
	if _, err := service.Complete(ctx, Completion{CommandID: command.ID, ClaimID: claim.ClaimID, Outcome: StatusFailed}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting completion error = %v", err)
	}

	confirmation.IdempotencyKey = "confirm-2"
	confirmation.ExpectedHead = "head"
	second, err := service.Create(ctx, project.ID, 7, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	secondClaim, err := service.ClaimNext(ctx, ClaimRequest{WorkerID: "worker", WorkerSessionID: "session", ClaimRequestID: "request-2"})
	if err != nil || secondClaim == nil {
		t.Fatalf("second claim = %#v, %v", secondClaim, err)
	}
	now = now.Add(2 * time.Minute)
	restarted := New(db, plans, binding, Config{Enabled: true, PendingTTL: 5 * time.Minute, ClaimTTL: time.Minute, Now: func() time.Time { return now }})
	if err := restarted.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM delivery_commands WHERE id=?", second.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusUncertain {
		t.Fatalf("status after lost acknowledgement = %q", status)
	}
}

func TestBrowserBindingResolverProjectsExactReadySnapshot(t *testing.T) {
	reader := fakeBindingReader{binding: browserbinding.Binding{
		BindingID: "binding", BindingVersion: 4, LaneKey: "acme/service#7:implementor", WorkerID: "worker",
		WorkerSessionID: "session", Readiness: "ready", PresenceToken: "presence",
		Target: browserbinding.TargetRef{Kind: browserbinding.TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/conversation"},
	}}
	resolved, err := NewBrowserBindingResolver(reader).Resolve(context.Background(), 1, reader.binding.LaneKey)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.BindingID != "binding" || resolved.BindingVersion != 4 || resolved.WorkerSessionID != "session" || resolved.TargetRef != "https://chatgpt.com/c/conversation" || resolved.PresenceToken != "presence" || !resolved.Ready {
		t.Fatalf("resolved binding = %#v", resolved)
	}
}

func TestDeliveryFallbackRequiresFinalApprovedPolicy(t *testing.T) {
	_, service, plans, _, projectID := deliveryFixture(t)
	plans.result.Status = planning.StatusFallback
	plans.result.PolicyDecision.Status = planning.StatusApproved
	command, err := service.Create(context.Background(), projectID, 7, fixtureConfirmation("fallback-approved"))
	if err != nil || command.Status != StatusPending {
		t.Fatalf("approved fallback = %#v, %v", command, err)
	}
	plans.result.PolicyDecision.Status = planning.StatusRejected
	if _, err := service.Create(context.Background(), projectID, 7, fixtureConfirmation("fallback-rejected")); !errors.Is(err, ErrConflict) {
		t.Fatalf("rejected fallback error = %v", err)
	}
}

func TestDeliveryRejectsStaleHeadContextAndLaneAtCreationAndClaim(t *testing.T) {
	ctx := context.Background()
	_, service, plans, _, projectID := deliveryFixture(t)
	confirmation := fixtureConfirmation("stale-head")
	plans.summary.CurrentHead = "new-head"
	if _, err := service.Create(ctx, projectID, 7, confirmation); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale head creation error = %v", err)
	}
	plans.summary.CurrentHead = "head"
	plans.summary.ContextHash = "new-context"
	if _, err := service.Create(ctx, projectID, 7, confirmation); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale context creation error = %v", err)
	}
	plans.summary.ContextHash = "context"
	plans.summary.Route.LaneKey = "acme/service#7:qa"
	if _, err := service.Create(ctx, projectID, 7, confirmation); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale lane creation error = %v", err)
	}

	for index, mutate := range []func(*fakePlans){
		func(value *fakePlans) { value.summary.CurrentHead = "new-head" },
		func(value *fakePlans) { value.summary.ContextHash = "new-context" },
		func(value *fakePlans) { value.summary.Route.LaneKey = "acme/service#7:qa" },
	} {
		_, service, plans, _, projectID := deliveryFixture(t)
		command, err := service.Create(ctx, projectID, 7, fixtureConfirmation(fmt.Sprintf("stale-claim-%d", index)))
		if err != nil {
			t.Fatal(err)
		}
		mutate(plans)
		claim, err := service.ClaimNext(ctx, ClaimRequest{WorkerID: "worker", WorkerSessionID: "session", ClaimRequestID: fmt.Sprintf("claim-stale-%d", index)})
		if err != nil || claim != nil {
			t.Fatalf("stale claim = %#v, %v", claim, err)
		}
		var status string
		if err := service.db.QueryRowContext(ctx, "SELECT status FROM delivery_commands WHERE id=?", command.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != StatusInvalidated {
			t.Fatalf("stale claim status = %q", status)
		}
	}
}

func TestDeliveryConcurrentDuplicateConfirmationAndFingerprintConflict(t *testing.T) {
	_, service, _, _, projectID := deliveryFixture(t)
	confirmation := fixtureConfirmation("concurrent-confirm")
	start := make(chan struct{})
	commands := make(chan Command, 8)
	errs := make(chan error, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			command, err := service.Create(context.Background(), projectID, 7, confirmation)
			if err != nil {
				errs <- err
			} else {
				commands <- command
			}
		}()
	}
	close(start)
	group.Wait()
	close(commands)
	close(errs)
	for err := range errs {
		t.Errorf("duplicate confirmation error = %v", err)
	}
	var first Command
	for command := range commands {
		if first.ID == "" {
			first = command
		} else if command.ID != first.ID {
			t.Fatalf("duplicate IDs = %q and %q", first.ID, command.ID)
		}
	}
	if first.ID == "" {
		t.Fatal("no command created")
	}
	changed := confirmation
	changed.ExpectedHead = "other"
	if _, err := service.Create(context.Background(), projectID, 7, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("same-key fingerprint conflict = %v", err)
	}
}

func TestDeliveryExpiryDisableAndProjectIsolation(t *testing.T) {
	ctx := context.Background()
	_, service, _, _, projectID := deliveryFixture(t)
	command, err := service.Create(ctx, projectID, 7, fixtureConfirmation("expiry"))
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 7, 26, 12, 6, 0, 0, time.UTC) }
	if err := service.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := service.db.QueryRowContext(ctx, "SELECT status FROM delivery_commands WHERE id=?", command.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusExpired {
		t.Fatalf("expired status = %q", status)
	}
	secondProject, err := supervisor.NewStore(service.db).CreateProject(ctx, supervisor.CreateProjectInput{Owner: "acme", Repository: "other-service", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	firstIsolated, err := service.Create(ctx, projectID, 7, fixtureConfirmation("isolation"))
	if err != nil {
		t.Fatal(err)
	}
	secondIsolated, err := service.Create(ctx, secondProject.ID, 7, fixtureConfirmation("isolation"))
	if err != nil || firstIsolated.ID == secondIsolated.ID {
		t.Fatalf("project isolation commands = %#v, %#v; err=%v", firstIsolated, secondIsolated, err)
	}
	secondCommands, err := service.List(ctx, secondProject.ID, 7)
	if err != nil || len(secondCommands) != 1 {
		t.Fatalf("project isolation list = %#v, err=%v", secondCommands, err)
	}

	service.enabled = false
	if _, err := service.Create(ctx, projectID, 7, fixtureConfirmation("disabled")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disabled create = %v", err)
	}
	if _, err := service.ClaimNext(ctx, ClaimRequest{WorkerID: "worker", WorkerSessionID: "session", ClaimRequestID: "disabled"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disabled claim = %v", err)
	}
	if err := service.db.QueryRowContext(ctx, "SELECT status FROM delivery_commands WHERE id=?", command.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusExpired {
		t.Fatalf("disable rewrote history to %q", status)
	}
}

type fakeBindingReader struct{ binding browserbinding.Binding }

func (f fakeBindingReader) Get(context.Context, int64, string) (browserbinding.Binding, error) {
	return f.binding, nil
}

const fixtureLane = "acme/service#7:implementor"

func fixtureConfirmation(key string) Confirmation {
	return Confirmation{PlanID: 4, IdempotencyKey: key, ExpectedPlanHash: "plan", ExpectedContextHash: "context", ExpectedHead: "head", ExpectedLaneKey: fixtureLane, ExpectedBindingID: "binding", ExpectedBindingVer: 1, ExpectedPresenceToken: "presence"}
}

func deliveryFixture(t *testing.T) (*sql.DB, *Service, *fakePlans, *fakeBindings, int64) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	project, err := supervisor.NewStore(db).CreateProject(ctx, supervisor.CreateProjectInput{Owner: "acme", Repository: "service", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	plans := &fakePlans{result: planning.GenerationResult{Status: planning.StatusApproved, Plan: &planning.PromptPlan{Action: "dispatch", TargetRole: "implementor", LaneKey: fixtureLane, ExpectedHead: "head", Prompt: "canonical prompt", Source: planning.SourceMetadata{ContextHash: "context"}}, PlanID: 4, PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, PlanHash: "plan"}}, summary: planning.ContextSummary{ContextHash: "context", CurrentHead: "head", Route: workflow.Route{Action: "dispatch", TargetRole: "implementor", LaneKey: fixtureLane}}}
	binding := &fakeBindings{value: BindingSnapshot{LaneKey: fixtureLane, BindingID: "binding", BindingVersion: 1, WorkerID: "worker", WorkerSessionID: "session", TargetKind: "chatgpt_conversation", TargetRef: "https://chatgpt.com/c/conversation", Ready: true, PresenceToken: "presence"}}
	return db, New(db, plans, binding, Config{Enabled: true, PendingTTL: 5 * time.Minute, ClaimTTL: time.Minute, Now: func() time.Time { return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC) }}), plans, binding, project.ID
}

func TestDeliveryInvalidatesStaleBindingBeforeClaim(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	project, err := supervisor.NewStore(db).CreateProject(ctx, supervisor.CreateProjectInput{Owner: "acme", Repository: "service", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	lane := "acme/service#7:implementor"
	plans := fakePlans{result: planning.GenerationResult{Status: planning.StatusApproved, Plan: &planning.PromptPlan{Action: "dispatch", TargetRole: "implementor", LaneKey: lane, ExpectedHead: "head", Prompt: "prompt", Source: planning.SourceMetadata{ContextHash: "context"}}, PlanID: 4, PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, PlanHash: "plan"}}, summary: planning.ContextSummary{ContextHash: "context", CurrentHead: "head", Route: workflow.Route{Action: "dispatch", TargetRole: "implementor", LaneKey: lane}}}
	binding := &fakeBindings{value: BindingSnapshot{LaneKey: lane, BindingID: "binding", BindingVersion: 1, WorkerID: "worker", WorkerSessionID: "session", TargetKind: "chat", TargetRef: "one", Ready: true, PresenceToken: "old"}}
	service := New(db, plans, binding, Config{Enabled: true, Now: func() time.Time { return now }})
	in := Confirmation{PlanID: 4, IdempotencyKey: "one", ExpectedPlanHash: "plan", ExpectedContextHash: "context", ExpectedHead: "head", ExpectedLaneKey: lane, ExpectedBindingID: "binding", ExpectedBindingVer: 1, ExpectedPresenceToken: "old"}
	command, err := service.Create(ctx, project.ID, 7, in)
	if err != nil {
		t.Fatal(err)
	}
	binding.value.PresenceToken = "new"
	claim, err := service.ClaimNext(ctx, ClaimRequest{WorkerID: "worker", WorkerSessionID: "session", ClaimRequestID: "claim"})
	if err != nil || claim != nil {
		t.Fatalf("stale claim = %#v, %v", claim, err)
	}
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM delivery_commands WHERE id=?", command.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusInvalidated {
		t.Fatalf("status = %q", status)
	}
}

func TestDeliveryCancelsOnlyPendingCommand(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	project, err := supervisor.NewStore(db).CreateProject(ctx, supervisor.CreateProjectInput{Owner: "acme", Repository: "service", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	lane := "acme/service#7:implementor"
	plans := fakePlans{result: planning.GenerationResult{Status: planning.StatusApproved, Plan: &planning.PromptPlan{Action: "dispatch", TargetRole: "implementor", LaneKey: lane, ExpectedHead: "head", Prompt: "prompt", Source: planning.SourceMetadata{ContextHash: "context"}}, PlanID: 4, PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, PlanHash: "plan"}}, summary: planning.ContextSummary{ContextHash: "context", CurrentHead: "head", Route: workflow.Route{Action: "dispatch", TargetRole: "implementor", LaneKey: lane}}}
	binding := fakeBindings{value: BindingSnapshot{LaneKey: lane, BindingID: "binding", BindingVersion: 1, WorkerID: "worker", WorkerSessionID: "session", TargetKind: "chat", TargetRef: "one", Ready: true, PresenceToken: "presence"}}
	service := New(db, plans, binding, Config{Enabled: true, Now: func() time.Time { return now }})
	command, err := service.Create(ctx, project.ID, 7, Confirmation{PlanID: 4, IdempotencyKey: "one", ExpectedPlanHash: "plan", ExpectedContextHash: "context", ExpectedHead: "head", ExpectedLaneKey: lane, ExpectedBindingID: "binding", ExpectedBindingVer: 1, ExpectedPresenceToken: "presence"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Cancel(ctx, command.ID)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancel = %#v, %v", cancelled, err)
	}
	if _, err := service.ClaimNext(ctx, ClaimRequest{WorkerID: "worker", WorkerSessionID: "session", ClaimRequestID: "claim"}); err != nil {
		t.Fatalf("claim after cancel = %v", err)
	}
}

func TestDeliveryConcurrentClaimsConsumeOneExecutionRight(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	project, err := supervisor.NewStore(db).CreateProject(ctx, supervisor.CreateProjectInput{Owner: "acme", Repository: "service", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	lane := "acme/service#7:implementor"
	plans := fakePlans{result: planning.GenerationResult{Status: planning.StatusApproved, Plan: &planning.PromptPlan{Action: "dispatch", TargetRole: "implementor", LaneKey: lane, ExpectedHead: "head", Prompt: "prompt", Source: planning.SourceMetadata{ContextHash: "context"}}, PlanID: 4, PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved, PlanHash: "plan"}}, summary: planning.ContextSummary{ContextHash: "context", CurrentHead: "head", Route: workflow.Route{Action: "dispatch", TargetRole: "implementor", LaneKey: lane}}}
	binding := fakeBindings{value: BindingSnapshot{LaneKey: lane, BindingID: "binding", BindingVersion: 1, WorkerID: "worker", WorkerSessionID: "session", TargetKind: "chat", TargetRef: "one", Ready: true, PresenceToken: "presence"}}
	service := New(db, plans, binding, Config{Enabled: true, Now: func() time.Time { return now }})
	_, err = service.Create(ctx, project.ID, 7, Confirmation{PlanID: 4, IdempotencyKey: "one", ExpectedPlanHash: "plan", ExpectedContextHash: "context", ExpectedHead: "head", ExpectedLaneKey: lane, ExpectedBindingID: "binding", ExpectedBindingVer: 1, ExpectedPresenceToken: "presence"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan *Execution, 8)
	errorsCh := make(chan error, 8)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			<-start
			claim, err := service.ClaimNext(ctx, ClaimRequest{WorkerID: "worker", WorkerSessionID: "session", ClaimRequestID: fmt.Sprintf("claim-%d", i)})
			if err != nil {
				errorsCh <- err
				return
			}
			results <- claim
		}(i)
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("claim error: %v", err)
	}
	count := 0
	for claim := range results {
		if claim != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("successful claims = %d, want 1", count)
	}
}
