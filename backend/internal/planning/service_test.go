package planning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

type fakePlannerResult struct {
	output string
	err    error
}

type fakePlanner struct {
	mu      sync.Mutex
	calls   int
	results []fakePlannerResult
	meta    PlannerMetadata
	block   <-chan struct{}
	started chan struct{}
	once    sync.Once
}

func (f *fakePlanner) Plan(_ context.Context, request PlannerRequest) (PlannerResponse, error) {
	f.mu.Lock()
	index := f.calls
	f.calls++
	var configured fakePlannerResult
	if index < len(f.results) {
		configured = f.results[index]
	}
	f.mu.Unlock()
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.block != nil {
		<-f.block
	}
	if configured.err != nil {
		return PlannerResponse{}, configured.err
	}
	if configured.output != "" {
		return PlannerResponse{Output: configured.output, Provider: "fake-provider", Model: "fake-model"}, nil
	}
	var contextValue PromptContext
	if err := json.Unmarshal(request.ContextJSON, &contextValue); err != nil {
		return PlannerResponse{}, err
	}
	plan := RenderFallback(contextValue)
	plan.Source = SourceMetadata{Kind: SourceOpenCode, Runtime: "opencode", Mode: ModeOpenCode, ContextHash: contextValue.ContextHash}
	encoded, err := json.Marshal(plan)
	return PlannerResponse{Output: string(encoded), Provider: "fake-provider", Model: "fake-model", Usage: Usage{InputTokens: 10, OutputTokens: 20, CostMicros: 50}}, err
}

func (f *fakePlanner) Health(context.Context) error { return nil }
func (f *fakePlanner) Metadata() PlannerMetadata {
	if f.meta.Runtime == "" {
		return PlannerMetadata{Enabled: true, Runtime: "opencode", Endpoint: "http://fake", Provider: "fake-provider", Model: "fake-model", Agent: "prompt-planner"}
	}
	return f.meta
}
func (f *fakePlanner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestServiceOpenCodeRepairFallbackAuditHistoryAndStaleness(t *testing.T) {
	t.Run("valid plan and regeneration history", func(t *testing.T) {
		service, store, db, project := newPlanningTestService(t, &fakePlanner{}, true, "repo-one")
		seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("a", 40), "Stage 4")

		first, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if first.Status != StatusApproved || first.Plan == nil || first.Plan.Source.Kind != SourceOpenCode {
			t.Fatalf("first result = %#v", first)
		}
		second, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
		if err != nil {
			t.Fatalf("second Generate() error = %v", err)
		}
		if second.PlanID == first.PlanID {
			t.Fatalf("regeneration reused plan id %d", first.PlanID)
		}
		history, err := service.History(context.Background(), project.ID, 11, 20)
		if err != nil || len(history) != 2 {
			t.Fatalf("History() = %#v, %v", history, err)
		}
		for _, table := range []string{"planning_generations", "model_invocations", "prompt_plans", "policy_decisions"} {
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			if count == 0 {
				t.Fatalf("%s has no audit rows", table)
			}
		}

		seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("b", 40), "Stage 4 changed")
		latest, err := service.Latest(context.Background(), project.ID, 11)
		if err != nil {
			t.Fatalf("Latest() error = %v", err)
		}
		if latest.Status != StatusStale || !violationContains(latest.PolicyDecision.Violations, "stale_context") {
			t.Fatalf("latest after changed Head/state = %#v", latest)
		}
	})

	t.Run("one bounded repair succeeds", func(t *testing.T) {
		planner := &fakePlanner{results: []fakePlannerResult{{output: "not-json"}, {}}}
		service, store, _, project := newPlanningTestService(t, planner, true, "repair-repo")
		seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("a", 40), "Stage 4")
		result, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != StatusApproved || planner.count() != 2 {
			t.Fatalf("result = %#v, calls = %d", result, planner.count())
		}
	})

	t.Run("second invalid response falls back", func(t *testing.T) {
		planner := &fakePlanner{results: []fakePlannerResult{{output: "not-json"}, {output: "still-not-json"}}}
		service, store, db, project := newPlanningTestService(t, planner, true, "fallback-repo")
		seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("a", 40), "Stage 4")
		result, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != StatusFallback || result.Plan == nil || result.Plan.Source.Kind != SourceTemplateFallback {
			t.Fatalf("fallback result = %#v", result)
		}
		if result.PolicyDecision.Status != StatusApproved || planner.count() != 2 {
			t.Fatalf("fallback policy/calls = %#v / %d", result.PolicyDecision, planner.count())
		}
		var policyCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM policy_decisions WHERE generation_id = ?", result.PlanID).Scan(&policyCount); err != nil {
			t.Fatal(err)
		}
		if policyCount != 3 {
			t.Fatalf("policy decision audit count = %d, want two rejected attempts plus final fallback approval", policyCount)
		}
	})

	t.Run("unavailable and disabled use fallback", func(t *testing.T) {
		for _, test := range []struct {
			name    string
			planner *fakePlanner
			calls   int
		}{
			{"unavailable", &fakePlanner{results: []fakePlannerResult{{err: &PlannerError{Category: "unavailable", Message: "down"}}}}, 1},
			{"timeout", &fakePlanner{results: []fakePlannerResult{{err: context.DeadlineExceeded}}}, 1},
			{"disabled", &fakePlanner{meta: PlannerMetadata{Enabled: false, Runtime: "opencode", Agent: "prompt-planner"}}, 0},
		} {
			t.Run(test.name, func(t *testing.T) {
				service, store, _, project := newPlanningTestService(t, test.planner, true, test.name+"-repo")
				seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("a", 40), "Stage 4")
				result, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
				if err != nil || result.Status != StatusFallback || test.planner.count() != test.calls {
					t.Fatalf("Generate() = %#v, %v; calls=%d", result, err, test.planner.count())
				}
			})
		}
	})

	t.Run("fallback disabled returns explicit planner error", func(t *testing.T) {
		tests := []struct {
			name       string
			planner    *fakePlanner
			calls      int
			wantStatus string
		}{
			{"transport error", &fakePlanner{results: []fakePlannerResult{{err: &PlannerError{Category: "unavailable", Message: "down"}}}}, 1, StatusPlannerError},
			{"repair exhausted", &fakePlanner{results: []fakePlannerResult{{output: "bad"}, {output: "bad-again"}}}, 2, StatusPlannerError},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				service, store, _, project := newPlanningTestService(t, test.planner, false, "error-"+strings.ReplaceAll(test.name, " ", "-")+"-repo")
				seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("a", 40), "Stage 4")
				result, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
				if err != nil || result.Status != test.wantStatus || result.Plan != nil || test.planner.count() != test.calls {
					t.Fatalf("Generate() = %#v, %v; calls=%d", result, err, test.planner.count())
				}
				if test.name == "repair exhausted" && !violationContains(result.PolicyDecision.Violations, "repair_exhausted") {
					t.Fatalf("repair exhaustion decision = %#v", result.PolicyDecision)
				}
			})
		}
	})
}

func TestServiceConcurrentDuplicateGenerationAndIsolation(t *testing.T) {
	t.Run("concurrent duplicate coalesces", func(t *testing.T) {
		block := make(chan struct{})
		planner := &fakePlanner{block: block, started: make(chan struct{})}
		service, store, _, project := newPlanningTestService(t, planner, true, "dedupe-repo")
		seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("a", 40), "Stage 4")

		results := make(chan GenerationResult, 2)
		errors := make(chan error, 2)
		go func() {
			result, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
			results <- result
			errors <- err
		}()
		<-planner.started
		go func() {
			result, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
			results <- result
			errors <- err
		}()
		time.Sleep(20 * time.Millisecond)
		close(block)
		first, second := <-results, <-results
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		if first.PlanID == 0 || first.PlanID != second.PlanID || planner.count() != 1 {
			t.Fatalf("plan ids %d/%d, planner calls %d", first.PlanID, second.PlanID, planner.count())
		}
		history, err := service.History(context.Background(), project.ID, 11, 20)
		if err != nil || len(history) != 1 {
			t.Fatalf("history = %#v, %v", history, err)
		}
	})

	t.Run("multi-repository isolation", func(t *testing.T) {
		ctx := context.Background()
		path := filepath.Join(t.TempDir(), "planning.db")
		db, err := database.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		store := supervisor.NewStore(db)
		firstProject, err := store.CreateProject(ctx, supervisor.CreateProjectInput{Owner: "NordCoder", Repository: "repo-a", WorkflowMode: "pull_request", PollingEnabled: false, PollIntervalSeconds: 300})
		if err != nil {
			t.Fatal(err)
		}
		secondProject, err := store.CreateProject(ctx, supervisor.CreateProjectInput{Owner: "NordCoder", Repository: "repo-b", WorkflowMode: "pull_request", PollingEnabled: false, PollIntervalSeconds: 300})
		if err != nil {
			t.Fatal(err)
		}
		seedPlanningSnapshot(t, store, firstProject.ID, 11, strings.Repeat("a", 40), "First")
		seedPlanningSnapshot(t, store, secondProject.ID, 11, strings.Repeat("b", 40), "Second")
		service := NewService(store, NewAuditStore(db), &fakePlanner{}, ServiceConfig{FallbackEnabled: true})
		first, err := service.Generate(ctx, firstProject.ID, 11, ModeFallback)
		if err != nil {
			t.Fatal(err)
		}
		second, err := service.Generate(ctx, secondProject.ID, 11, ModeFallback)
		if err != nil {
			t.Fatal(err)
		}
		if first.Context.Repository.ProjectID == second.Context.Repository.ProjectID || first.Context.Repository.Repository == second.Context.Repository.Repository {
			t.Fatalf("contexts crossed repositories: %#v / %#v", first.Context.Repository, second.Context.Repository)
		}
		firstHistory, _ := service.History(ctx, firstProject.ID, 11, 20)
		secondHistory, _ := service.History(ctx, secondProject.ID, 11, 20)
		if len(firstHistory) != 1 || len(secondHistory) != 1 || firstHistory[0].PlanID == secondHistory[0].PlanID {
			t.Fatalf("isolated history = %#v / %#v", firstHistory, secondHistory)
		}
		if _, err := service.Get(ctx, firstProject.ID, 11, second.PlanID); !errors.Is(err, ErrPlanNotFound) {
			t.Fatalf("cross-project Get() error = %v, want not found", err)
		}
	})
}

func newPlanningTestService(t *testing.T, planner PromptPlanner, fallback bool, repository string) (*Service, *supervisor.Store, *sql.DB, supervisor.Project) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "planning.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := supervisor.NewStore(db)
	project, err := store.CreateProject(ctx, supervisor.CreateProjectInput{Owner: "NordCoder", Repository: repository, WorkflowMode: "pull_request", PollingEnabled: false, PollIntervalSeconds: 300})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store, NewAuditStore(db), planner, ServiceConfig{FallbackEnabled: fallback}), store, db, project
}

func seedPlanningSnapshot(t *testing.T, store *supervisor.Store, projectID int64, issueNumber int, head, title string) {
	t.Helper()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	githubBase := projectID * 1000
	err := store.ReplaceSnapshot(context.Background(), projectID, supervisor.RepositorySnapshot{
		FetchedAt: now,
		Issues: []supervisor.Issue{{
			GitHubID: githubBase + int64(issueNumber), Number: issueNumber, Title: title,
			Body: "# Outcome\n\nImplement the authoritative Stage 4 contract from the persisted Issue body.", State: "open",
			URL: "https://example.invalid/issues/11", Author: "owner", CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			Labels: []supervisor.Label{{Name: "implementation", Color: "ffffff"}}, Comments: []supervisor.Comment{},
			PullRequests: []supervisor.PullRequest{{
				GitHubID: githubBase + 500, Number: issueNumber + 100, Title: "Candidate", State: "open", Draft: true,
				BaseRef: "main", HeadRef: "stage-4", HeadSHA: head, URL: "https://example.invalid/pull", UpdatedAt: now,
				CI: supervisor.CISummary{HeadSHA: head, Status: "queued", Source: "check_runs", UpdatedAt: now},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("ReplaceSnapshot() error = %v", err)
	}
}
