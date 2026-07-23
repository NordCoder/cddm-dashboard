package workflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

var qaTestTime = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

func TestIndentedMarkdownCodeIsNotOperationalEvidence(t *testing.T) {
	head := strings.Repeat("a", 40)
	bodies := []string{
		"    <!-- supervisor:event\n    {\"v\":1,\"event\":\"worker_result\",\"role\":\"qa\",\"status\":\"completed\",\"head\":\"" + head + "\",\"verdict\":\"approved\"}\n    -->",
		"\t<!-- supervisor:event\n\t{\"v\":1,\"event\":\"worker_result\",\"role\":\"qa\",\"status\":\"completed\",\"head\":\"" + head + "\",\"verdict\":\"approved\"}\n\t-->",
		"    ## QA Verdict\n    Head: `" + head + "`\n    Verdict: approved",
	}
	for index, body := range bodies {
		parsed := ParseComment(1, 6, testComment(int64(index+1), qaTestTime, body))
		if parsed.Event != nil || parsed.Level != ParseLevelActivity {
			t.Fatalf("indented example %d became operational evidence: %#v", index, parsed)
		}
	}
}

func TestActiveBlockerRequiresActionableCorrelatedLeadResolution(t *testing.T) {
	head := strings.Repeat("b", 40)

	t.Run("resolves with non-actionable decision does not clear", func(t *testing.T) {
		issue := testIssue(head,
			testEvent(1, qaTestTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"}),
			testEvent(2, qaTestTime.Add(time.Minute), map[string]any{"role": "lead", "status": "completed", "decision": "observe", "resume_role": "qa", "resolves": 1}),
		)
		state := DeriveProject(testProject(issue)).WorkUnits[0]
		if state.ActiveBlocker == nil || state.ActiveBlocker.CommentID != 1 {
			t.Fatalf("non-actionable Lead result cleared blocker: %#v", state.ActiveBlocker)
		}
		if state.Route.Action != "manual_attention" || state.Route.ReasonCode != "unresolved_active_blocker" {
			t.Fatalf("route = %#v", state.Route)
		}
		assertTestWarning(t, state.Warnings, "non_actionable_blocker_resolution")
	})

	t.Run("mismatched owner escalation does not replace worker blocker", func(t *testing.T) {
		issue := testIssue(head,
			testEvent(1, qaTestTime, map[string]any{"role": "implementor", "status": "blocked"}),
			testEvent(2, qaTestTime.Add(time.Minute), map[string]any{"role": "lead", "status": "blocked", "decision": "owner_required", "escalate_to": "owner", "resolves": 999}),
		)
		state := DeriveProject(testProject(issue)).WorkUnits[0]
		if state.ActiveBlocker == nil || state.ActiveBlocker.CommentID != 1 {
			t.Fatalf("mismatched owner escalation replaced blocker: %#v", state.ActiveBlocker)
		}
		if state.Route.Action == "owner_attention" || state.Route.ReasonCode != "unresolved_active_blocker" {
			t.Fatalf("unsafe owner escalation route = %#v", state.Route)
		}
		assertTestWarning(t, state.Warnings, "unmatched_blocker_resolution")
		assertTestWarning(t, state.Warnings, "additional_unresolved_blocker")
	})

	t.Run("matched continuation still clears and resumes", func(t *testing.T) {
		issue := testIssue(head,
			testEvent(1, qaTestTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"}),
			testEvent(2, qaTestTime.Add(time.Minute), map[string]any{"role": "lead", "status": "completed", "decision": "continue", "resume_role": "qa", "resolves": 1}),
		)
		state := DeriveProject(testProject(issue)).WorkUnits[0]
		if state.ActiveBlocker != nil || state.Route.Action != "dispatch" || state.Route.TargetRole != "qa" {
			t.Fatalf("matched resolution failed: blocker=%#v route=%#v", state.ActiveBlocker, state.Route)
		}
	})
}

func testProject(issue supervisor.Issue) supervisor.ProjectSnapshot {
	return supervisor.ProjectSnapshot{
		Project: supervisor.Project{ID: 1, Owner: "acme", Repository: "service", WorkflowMode: "pull_request"},
		Issues:  []supervisor.Issue{issue},
	}
}

func testIssue(head string, comments ...supervisor.Comment) supervisor.Issue {
	return supervisor.Issue{
		GitHubID:  600,
		Number:    6,
		Title:     "Stage 3",
		State:     "open",
		URL:       "https://example/issues/6",
		CreatedAt: qaTestTime,
		UpdatedAt: qaTestTime,
		Labels:    []supervisor.Label{{Name: "implementation"}},
		Comments:  append([]supervisor.Comment{testComment(99, qaTestTime.Add(-time.Hour), "## Lead Dispatch\n\nProceed.")}, comments...),
		PullRequests: []supervisor.PullRequest{{
			GitHubID:  700,
			Number:    7,
			Title:     "Stage 3",
			State:     "open",
			Draft:     true,
			BaseRef:   "main",
			HeadRef:   "stage-3",
			HeadSHA:   head,
			URL:       "https://example/pr/7",
			UpdatedAt: qaTestTime,
			CI:        supervisor.CISummary{HeadSHA: head, Status: "completed", Conclusion: "success", UpdatedAt: qaTestTime},
		}},
	}
}

func testEvent(id int64, at time.Time, fields map[string]any) supervisor.Comment {
	payload := map[string]any{"v": 1, "event": "worker_result"}
	for key, value := range fields {
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return testComment(id, at, "<!-- supervisor:event\n"+string(encoded)+"\n-->")
}

func testComment(id int64, at time.Time, body string) supervisor.Comment {
	return supervisor.Comment{GitHubID: id, Body: body, Author: "worker", URL: "https://example/comments", CreatedAt: at, UpdatedAt: at}
}

func assertTestWarning(t *testing.T, warnings []Warning, code string) {
	t.Helper()
	for _, item := range warnings {
		if item.Code == code {
			return
		}
	}
	t.Fatalf("warning %q not found in %#v", code, warnings)
}
