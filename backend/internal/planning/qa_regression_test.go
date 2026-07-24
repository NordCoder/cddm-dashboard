package planning

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

func TestPersistedIssueBodyDrivesContextHashAndFallbackObjective(t *testing.T) {
	service, store, _, project := newPlanningTestService(t, &fakePlanner{}, true, "issue-body-repo")
	seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("a", 40), "Stage 4")

	result, err := service.Generate(context.Background(), project.ID, 11, ModeFallback)
	if err != nil {
		t.Fatal(err)
	}
	const objective = "Implement the authoritative Stage 4 contract from the persisted Issue body."
	if !strings.Contains(result.Context.Issue.Body, objective) {
		t.Fatalf("context Issue body = %q", result.Context.Issue.Body)
	}
	if result.Plan == nil || !strings.Contains(result.Plan.Prompt, objective) {
		t.Fatalf("fallback prompt does not carry authoritative objective: %#v", result.Plan)
	}
	firstHash := result.Context.ContextHash

	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	head := strings.Repeat("a", 40)
	if err := store.ReplaceSnapshot(context.Background(), project.ID, supervisor.RepositorySnapshot{
		FetchedAt: now,
		Issues: []supervisor.Issue{{
			GitHubID: project.ID*1000 + 11, Number: 11, Title: "Stage 4",
			Body: "# Outcome\n\nA changed authoritative objective.", State: "open",
			URL: "https://example.invalid/issues/11", Author: "owner", CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			Labels: []supervisor.Label{{Name: "implementation", Color: "ffffff"}},
			PullRequests: []supervisor.PullRequest{{
				GitHubID: project.ID*1000 + 500, Number: 111, Title: "Candidate", State: "open", Draft: true,
				BaseRef: "main", HeadRef: "stage-4", HeadSHA: head, URL: "https://example.invalid/pull", UpdatedAt: now,
				CI: supervisor.CISummary{HeadSHA: head, Status: "queued", Source: "check_runs", UpdatedAt: now},
			}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := service.ContextSummary(context.Background(), project.ID, 11)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ContextHash == firstHash {
		t.Fatal("changed persisted Issue body did not change context hash")
	}
}

func TestIssueBodyIsBoundedAndRedacted(t *testing.T) {
	body := "GITHUB_TOKEN=ghp_12345678901234567890\n{\"password\":\"json-secret\"}\n" + strings.Repeat("objective ", 100)
	snapshot := supervisor.ProjectSnapshot{
		Project: supervisor.Project{ID: 1, Owner: "acme", Repository: "service", WorkflowMode: "pull_request"},
		Issues:  []supervisor.Issue{{GitHubID: 11, Number: 11, Title: "Stage 4", Body: body}},
	}
	state := workflow.WorkUnitState{
		Identity:  workflow.WorkUnitIdentity{ProjectID: 1, Owner: "acme", Repository: "service", IssueGitHubID: 11, IssueNumber: 11, Title: "Stage 4"},
		Lifecycle: "implementation", Attention: workflow.Attention{Kind: workflow.AttentionActionRequired, Code: "work"},
		Route:    workflow.Route{Action: "dispatch", TargetRole: "implementor", LaneKey: "acme/service#11:implementor", ReasonCode: "work", Reason: "work", Guards: []string{}, Warnings: []workflow.Warning{}},
		Warnings: []workflow.Warning{}, ParsedComments: []workflow.ParsedComment{},
	}
	contextValue, _, err := BuildContext(snapshot, state, ContextOptions{EvidenceLimit: 8, EvidenceChars: 256})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(contextValue.Issue.Body, "ghp_12345678901234567890") || strings.Contains(contextValue.Issue.Body, "json-secret") || !strings.Contains(contextValue.Issue.Body, "[REDACTED]") {
		t.Fatalf("Issue body was not redacted: %q", contextValue.Issue.Body)
	}
	if !strings.Contains(contextValue.Issue.Body, "…[truncated]") {
		t.Fatalf("Issue body was not bounded: %q", contextValue.Issue.Body)
	}
}

func TestModelInvocationPersistsHashWithoutRawResponse(t *testing.T) {
	const firstOutput = `{"password":"raw-secret-one"}`
	const secondOutput = `{"nested":{"authorization":"raw-secret-two"}}`
	planner := &fakePlanner{results: []fakePlannerResult{{output: firstOutput}, {output: secondOutput}}}
	service, store, db, project := newPlanningTestService(t, planner, true, "safe-audit-repo")
	seedPlanningSnapshot(t, store, project.ID, 11, strings.Repeat("a", 40), "Stage 4")

	result, err := service.Generate(context.Background(), project.ID, 11, ModeOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusFallback {
		t.Fatalf("status = %q, want fallback", result.Status)
	}
	rows, err := db.Query(`SELECT attempt, response_hash, response_text FROM model_invocations WHERE generation_id = ? ORDER BY attempt`, result.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	expected := []string{hashBytes([]byte(firstOutput)), hashBytes([]byte(secondOutput))}
	count := 0
	for rows.Next() {
		var attempt int
		var responseHash, responseText string
		if err := rows.Scan(&attempt, &responseHash, &responseText); err != nil {
			t.Fatal(err)
		}
		if responseText != "" {
			t.Fatalf("attempt %d persisted raw response %q", attempt, responseText)
		}
		if attempt < 0 || attempt >= len(expected) || responseHash != expected[attempt] {
			t.Fatalf("attempt %d hash = %q, want %q", attempt, responseHash, expected[attempt])
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("invocation count = %d, want 2", count)
	}
}
