package planning

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

type ServiceConfig struct {
	ContextOptions  ContextOptions
	FallbackEnabled bool
}

type Service struct {
	snapshots *supervisor.Store
	audit     *AuditStore
	planner   PromptPlanner
	config    ServiceConfig
	now       func() time.Time

	mu       sync.Mutex
	inflight map[string]*generationCall
}

type generationCall struct {
	done   chan struct{}
	result GenerationResult
	err    error
}

func NewService(snapshots *supervisor.Store, audit *AuditStore, planner PromptPlanner, config ServiceConfig) *Service {
	return &Service{
		snapshots: snapshots, audit: audit, planner: planner, config: config,
		now: func() time.Time { return time.Now().UTC() }, inflight: make(map[string]*generationCall),
	}
}

func (s *Service) Generate(ctx context.Context, projectID int64, issueNumber int, mode string) (GenerationResult, error) {
	if mode == "" {
		mode = ModeOpenCode
	}
	if mode != ModeOpenCode && mode != ModeFallback {
		return GenerationResult{}, fmt.Errorf("planning mode must be opencode or fallback")
	}
	contextValue, contextJSON, err := s.freshContext(ctx, projectID, issueNumber)
	if err != nil {
		return GenerationResult{}, err
	}
	key := fmt.Sprintf("%d/%d/%s/%s", projectID, issueNumber, contextValue.ContextHash, mode)

	s.mu.Lock()
	if existing := s.inflight[key]; existing != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return GenerationResult{}, ctx.Err()
		case <-existing.done:
			return existing.result, existing.err
		}
	}
	call := &generationCall{done: make(chan struct{})}
	s.inflight[key] = call
	s.mu.Unlock()

	call.result, call.err = s.generate(ctx, projectID, issueNumber, mode, contextValue, contextJSON)
	close(call.done)
	s.mu.Lock()
	delete(s.inflight, key)
	s.mu.Unlock()
	return call.result, call.err
}

func (s *Service) generate(ctx context.Context, projectID int64, issueNumber int, mode string, contextValue PromptContext, contextJSON []byte) (GenerationResult, error) {
	createdAt := s.now()
	if mode == ModeFallback {
		return s.persistFallback(ctx, projectID, issueNumber, mode, contextValue, contextJSON, nil, nil, createdAt)
	}
	metadata := s.planner.Metadata()
	if !metadata.Enabled {
		if s.config.FallbackEnabled {
			return s.persistFallback(ctx, projectID, issueNumber, mode, contextValue, contextJSON,
				[]InvocationRecord{{Attempt: 0, Runtime: metadata.Runtime, Provider: metadata.Provider, Model: metadata.Model, Agent: metadata.Agent, Mode: mode, Status: "skipped", ErrorCategory: "disabled", StartedAt: createdAt, CompletedAt: createdAt}}, nil, createdAt)
		}
		return s.persistPlannerError(ctx, projectID, issueNumber, mode, contextValue, contextJSON, nil, nil, "disabled", createdAt)
	}

	invocations := make([]InvocationRecord, 0, 2)
	decisionHistory := make([]PolicyAuditDecision, 0, 2)
	var lastDecision PolicyDecision
	for attempt := 0; attempt < 2; attempt++ {
		started := s.now()
		response, plannerErr := s.planner.Plan(ctx, PlannerRequest{ContextJSON: contextJSON, Attempt: attempt, Violations: lastDecision.Violations})
		completed := s.now()
		invocation := InvocationRecord{
			Attempt: attempt, Runtime: metadata.Runtime, Provider: metadata.Provider, Model: metadata.Model,
			Agent: metadata.Agent, Mode: mode, Latency: completed.Sub(started), StartedAt: started, CompletedAt: completed,
		}
		if plannerErr != nil {
			invocation.Status = "error"
			invocation.ErrorCategory = errorCategory(plannerErr)
			invocations = append(invocations, invocation)
			if s.config.FallbackEnabled {
				return s.persistFallback(ctx, projectID, issueNumber, mode, contextValue, contextJSON, invocations, decisionHistory, createdAt)
			}
			return s.persistPlannerError(ctx, projectID, issueNumber, mode, contextValue, contextJSON, invocations, decisionHistory, invocation.ErrorCategory, createdAt)
		}
		invocation.Status = "completed"
		invocation.Provider = response.Provider
		invocation.Model = response.Model
		invocation.Usage = response.Usage
		invocation.Output = response.Output
		invocations = append(invocations, invocation)

		plan, planJSON, parseErr := ParsePlan(response.Output)
		if parseErr != nil {
			lastDecision = PolicyDecision{
				Status: StatusRejected, ContextHash: contextValue.ContextHash, DecidedAt: completed,
				Violations: []Violation{{Code: "malformed_json", Field: "response", Message: redactText(parseErr.Error())}},
			}
			decisionHistory = append(decisionHistory, PolicyAuditDecision{Attempt: attempt, Decision: lastDecision})
			if attempt == 0 {
				continue
			}
			break
		}
		plan.Source = SourceMetadata{
			Kind: SourceOpenCode, Runtime: "opencode", Provider: response.Provider, Model: response.Model,
			Agent: metadata.Agent, Mode: ModeOpenCode, ContextHash: contextValue.ContextHash,
		}
		planJSON, _ = CanonicalPlanBytes(plan)
		current, _, currentErr := s.freshContext(ctx, projectID, issueNumber)
		if currentErr != nil {
			return GenerationResult{}, currentErr
		}
		lastDecision = ValidatePlan(contextValue, plan, current, completed)
		if lastDecision.Status == StatusApproved {
			id, err := s.audit.Save(ctx, GenerationRecord{
				ProjectID: projectID, IssueNumber: issueNumber, Mode: mode, Status: StatusApproved,
				Context: contextValue, ContextJSON: contextJSON, Plan: &plan, PlanJSON: planJSON,
				Decision: lastDecision, DecisionHistory: decisionHistory, Invocations: invocations, CreatedAt: createdAt,
			})
			if err != nil {
				return GenerationResult{}, err
			}
			return GenerationResult{Status: StatusApproved, Context: contextValue, Plan: &plan, PolicyDecision: lastDecision, PlanID: id, CreatedAt: createdAt}, nil
		}
		if lastDecision.Status == StatusStale {
			id, err := s.audit.Save(ctx, GenerationRecord{
				ProjectID: projectID, IssueNumber: issueNumber, Mode: mode, Status: StatusStale,
				Context: contextValue, ContextJSON: contextJSON, Plan: &plan, PlanJSON: planJSON,
				Decision: lastDecision, DecisionHistory: decisionHistory, Invocations: invocations, CreatedAt: createdAt,
			})
			if err != nil {
				return GenerationResult{}, err
			}
			return GenerationResult{Status: StatusStale, Context: contextValue, Plan: &plan, PolicyDecision: lastDecision, PlanID: id, CreatedAt: createdAt}, nil
		}
		decisionHistory = append(decisionHistory, PolicyAuditDecision{Attempt: attempt, Decision: lastDecision})
	}

	if s.config.FallbackEnabled {
		return s.persistFallback(ctx, projectID, issueNumber, mode, contextValue, contextJSON, invocations, decisionHistory, createdAt)
	}
	violations := append([]Violation(nil), lastDecision.Violations...)
	violations = append(violations, Violation{Code: "repair_exhausted", Field: "response", Message: "OpenCode remained invalid after the single repair attempt"})
	finalDecision := PolicyDecision{
		Status: StatusRejected, ContextHash: contextValue.ContextHash, DecidedAt: s.now(), Violations: violations,
	}
	id, err := s.audit.Save(ctx, GenerationRecord{
		ProjectID: projectID, IssueNumber: issueNumber, Mode: mode, Status: StatusRejected,
		Context: contextValue, ContextJSON: contextJSON, Decision: finalDecision, DecisionHistory: decisionHistory,
		Invocations: invocations, CreatedAt: createdAt,
	})
	if err != nil {
		return GenerationResult{}, err
	}
	return GenerationResult{Status: StatusRejected, Context: contextValue, PolicyDecision: finalDecision, PlanID: id, CreatedAt: createdAt}, nil
}

func (s *Service) persistFallback(ctx context.Context, projectID int64, issueNumber int, requestedMode string, contextValue PromptContext, contextJSON []byte, invocations []InvocationRecord, history []PolicyAuditDecision, createdAt time.Time) (GenerationResult, error) {
	plan := RenderFallback(contextValue)
	planJSON, err := CanonicalPlanBytes(plan)
	if err != nil {
		return GenerationResult{}, err
	}
	current, _, err := s.freshContext(ctx, projectID, issueNumber)
	if err != nil {
		return GenerationResult{}, err
	}
	decision := ValidatePlan(contextValue, plan, current, s.now())
	status := StatusFallback
	if decision.Status != StatusApproved {
		status = decision.Status
	}
	id, err := s.audit.Save(ctx, GenerationRecord{
		ProjectID: projectID, IssueNumber: issueNumber, Mode: requestedMode, Status: status,
		Context: contextValue, ContextJSON: contextJSON, Plan: &plan, PlanJSON: planJSON,
		Decision: decision, DecisionHistory: history, Invocations: invocations, CreatedAt: createdAt,
	})
	if err != nil {
		return GenerationResult{}, err
	}
	return GenerationResult{Status: status, Context: contextValue, Plan: &plan, PolicyDecision: decision, PlanID: id, CreatedAt: createdAt}, nil
}

func (s *Service) persistPlannerError(ctx context.Context, projectID int64, issueNumber int, mode string, contextValue PromptContext, contextJSON []byte, invocations []InvocationRecord, history []PolicyAuditDecision, category string, createdAt time.Time) (GenerationResult, error) {
	decision := PolicyDecision{
		Status: StatusPlannerError, ContextHash: contextValue.ContextHash, DecidedAt: s.now(),
		Violations: []Violation{{Code: "planner_error", Field: "runtime", Message: category}},
	}
	id, err := s.audit.Save(ctx, GenerationRecord{
		ProjectID: projectID, IssueNumber: issueNumber, Mode: mode, Status: StatusPlannerError,
		Context: contextValue, ContextJSON: contextJSON, Decision: decision, DecisionHistory: history,
		Invocations: invocations, CreatedAt: createdAt,
	})
	if err != nil {
		return GenerationResult{}, err
	}
	return GenerationResult{Status: StatusPlannerError, Context: contextValue, PolicyDecision: decision, PlanID: id, CreatedAt: createdAt}, nil
}

func (s *Service) Latest(ctx context.Context, projectID int64, issueNumber int) (GenerationResult, error) {
	record, err := s.audit.Latest(ctx, projectID, issueNumber)
	if err != nil {
		return GenerationResult{}, err
	}
	return s.resultWithStaleness(ctx, record)
}

func (s *Service) Get(ctx context.Context, projectID int64, issueNumber int, generationID int64) (GenerationResult, error) {
	record, err := s.audit.Get(ctx, projectID, issueNumber, generationID)
	if err != nil {
		return GenerationResult{}, err
	}
	return s.resultWithStaleness(ctx, record)
}

func (s *Service) History(ctx context.Context, projectID int64, issueNumber, limit int) ([]GenerationResult, error) {
	records, err := s.audit.History(ctx, projectID, issueNumber, limit)
	if err != nil {
		return nil, err
	}
	results := make([]GenerationResult, 0, len(records))
	for _, record := range records {
		result, err := s.resultWithStaleness(ctx, record)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Service) resultWithStaleness(ctx context.Context, record GenerationRecord) (GenerationResult, error) {
	result := GenerationResult{
		Status: record.Status, Context: record.Context, Plan: record.Plan,
		PolicyDecision: record.Decision, PlanID: record.ID, CreatedAt: record.CreatedAt,
	}
	current, _, err := s.freshContext(ctx, record.ProjectID, record.IssueNumber)
	if IsNotFound(err) {
		result.Status = StatusStale
		result.PolicyDecision.Status = StatusStale
		result.PolicyDecision.DecidedAt = s.now()
		result.PolicyDecision.Violations = []Violation{{Code: "stale_work_unit", Field: "issue", Message: "work unit is no longer present in the persisted GitHub snapshot"}}
		return result, nil
	}
	if err != nil {
		return GenerationResult{}, err
	}
	if current.ContextHash != record.Context.ContextHash {
		result.Status = StatusStale
		result.PolicyDecision.Status = StatusStale
		result.PolicyDecision.DecidedAt = s.now()
		result.PolicyDecision.Violations = []Violation{{Code: "stale_context", Field: "context_hash", Message: "stored plan is not valid for the current authoritative context"}}
	}
	return result, nil
}

func (s *Service) ContextSummary(ctx context.Context, projectID int64, issueNumber int) (ContextSummary, error) {
	value, _, err := s.freshContext(ctx, projectID, issueNumber)
	if err != nil {
		return ContextSummary{}, err
	}
	return ContextSummary{
		Version: value.Version, ContextHash: value.ContextHash, Repository: value.Repository, Issue: value.Issue,
		CurrentHead: value.CurrentHead, Route: value.Route, ExpectedEvent: value.ExpectedEvent,
		EvidenceCount: len(value.Evidence), WarningCount: len(value.Warnings),
	}, nil
}

func (s *Service) Health(ctx context.Context) Health {
	metadata := s.planner.Metadata()
	response := Health{
		Enabled: metadata.Enabled, Runtime: metadata.Runtime, Endpoint: metadata.Endpoint,
		Provider: metadata.Provider, Model: metadata.Model, Agent: metadata.Agent,
	}
	if !metadata.Enabled {
		response.Status = "disabled"
		return response
	}
	if err := s.planner.Health(ctx); err != nil {
		response.Status = "unavailable"
		response.Error = errorCategory(err)
		return response
	}
	response.Status = "healthy"
	return response
}

func (s *Service) freshContext(ctx context.Context, projectID int64, issueNumber int) (PromptContext, []byte, error) {
	snapshot, err := s.snapshots.ProjectSnapshot(ctx, projectID)
	if err != nil {
		return PromptContext{}, nil, err
	}
	state := workflow.DeriveProject(snapshot)
	workUnit, ok := workflow.FindWorkUnit(state, issueNumber)
	if !ok {
		return PromptContext{}, nil, ErrPlanNotFound
	}
	return BuildContext(snapshot, workUnit, s.config.ContextOptions)
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrPlanNotFound) || errors.Is(err, supervisor.ErrNotFound)
}
