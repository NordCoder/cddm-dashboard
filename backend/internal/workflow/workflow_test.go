package workflow

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

var testTime = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

func TestParseAuthoritativeEnvelopePreservesExtensions(t *testing.T) {
	parsed := ParseComment(7, 6, comment(101, testTime, `## Implementor Handoff

Done.

<!-- supervisor:event
{"v":1,"event":"worker_result","role":"implementor","status":"completed","head":"abc123","checked":["tests"],"future_mode":"safe"}
-->`))

	if parsed.HardError != nil || !parsed.TransitionSafe || parsed.Level != ParseLevelEnvelope {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed.Markdown != "## Implementor Handoff\n\nDone." {
		t.Fatalf("Markdown = %q", parsed.Markdown)
	}
	if parsed.Event.Head != "abc123" || len(parsed.Event.Extensions) != 2 {
		t.Fatalf("event = %#v", parsed.Event)
	}
	if _, ok := parsed.Event.Extensions["checked"]; !ok {
		t.Fatalf("extensions = %#v", parsed.Event.Extensions)
	}
}

func TestParseHardErrorsAreLimitedAndContained(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "malformed JSON", body: `<!-- supervisor:event {"v":1,} -->`, code: "malformed_envelope"},
		{name: "missing QA head", body: `<!-- supervisor:event {"v":1,"event":"worker_result","role":"qa","status":"completed","verdict":"approved"} -->`, code: "missing_required_field"},
		{name: "missing closing marker", body: `<!-- supervisor:event {"v":1}`, code: "malformed_envelope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := ParseComment(1, 6, comment(1, testTime, test.body))
			if parsed.HardError == nil || parsed.HardError.Code != test.code || parsed.TransitionSafe {
				t.Fatalf("parsed = %#v", parsed)
			}
		})
	}
}

func TestParseUnknownRequiredValuesBecomeWarnings(t *testing.T) {
	parsed := ParseComment(1, 6, comment(2, testTime, `<!-- supervisor:event
{"v":2,"event":"future_result","role":"auditor","status":"paused","extra":{"x":1}}
-->`))
	if parsed.HardError != nil {
		t.Fatalf("HardError = %#v", parsed.HardError)
	}
	if parsed.TransitionSafe {
		t.Fatal("unknown required values must not auto-transition")
	}
	assertWarning(t, parsed.Warnings, "unknown_version")
	assertWarning(t, parsed.Warnings, "unknown_event")
	assertWarning(t, parsed.Warnings, "unknown_role")
	assertWarning(t, parsed.Warnings, "unknown_status")
	if _, ok := parsed.Event.Extensions["extra"]; !ok {
		t.Fatalf("extensions = %#v", parsed.Event.Extensions)
	}
}

func TestFallbackHeadingsAndUnclassifiedActivity(t *testing.T) {
	legacy := ParseComment(1, 6, comment(3, testTime, "## QA Verdict\n\nHead: `abc`\nVerdict: approved"))
	if legacy.Level != ParseLevelHeading || legacy.Event == nil || legacy.Event.Role != "qa" || legacy.Event.Verdict != "approved" || !legacy.TransitionSafe {
		t.Fatalf("legacy = %#v", legacy)
	}
	assertWarning(t, legacy.Warnings, "legacy_heading")

	fencedFields := ParseComment(1, 6, comment(30, testTime, "## QA Verdict\n\n```text\nHead: abc\nVerdict: approved\n```"))
	if fencedFields.Event == nil || fencedFields.Event.Head != "" || fencedFields.Event.Verdict != "" || fencedFields.TransitionSafe {
		t.Fatalf("fenced legacy fields became operational evidence: %#v", fencedFields)
	}

	activity := ParseComment(1, 6, comment(4, testTime, "Investigated CI and added context."))
	if activity.Level != ParseLevelActivity || !activity.Meaningful || activity.Event != nil {
		t.Fatalf("activity = %#v", activity)
	}
	if len(activity.Warnings) != 0 {
		t.Fatalf("unclassified activity warnings = %#v", activity.Warnings)
	}
}

func TestRoleMismatchAndMissingDispatchAreSoftWarnings(t *testing.T) {
	head := fullHead("7")
	mismatch := ParseComment(1, 6, comment(5, testTime, "## Implementor Handoff\n\n<!-- supervisor:event\n{\"v\":1,\"event\":\"worker_result\",\"role\":\"qa\",\"status\":\"completed\",\"head\":\""+head+"\",\"verdict\":\"approved\"}\n-->"))
	if mismatch.HardError != nil || mismatch.TransitionSafe {
		t.Fatalf("mismatch = %#v", mismatch)
	}
	assertWarning(t, mismatch.Warnings, "role_mismatch")

	issue := issueWith(head, eventComment(6, testTime, map[string]any{"role": "implementor", "status": "completed", "head": head}))
	issue.Comments = issue.Comments[1:] // remove the default Lead Dispatch
	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	assertWarning(t, state.Warnings, "missing_dispatch_correlation")
	if state.Route.TargetRole != "qa" {
		t.Fatalf("soft warning unexpectedly stopped safe route: %#v", state.Route)
	}
}

func TestRoutingMatrix(t *testing.T) {
	head := fullHead("a")
	tests := []struct {
		name       string
		comments   []supervisor.Comment
		wantAction string
		wantRole   string
		wantCode   string
	}{
		{
			name:       "implementor completed to QA",
			comments:   []supervisor.Comment{eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": head})},
			wantAction: "dispatch", wantRole: "qa", wantCode: "implementation_completed",
		},
		{
			name:       "implementor no-op to QA without loop",
			comments:   []supervisor.Comment{eventComment(1, testTime, map[string]any{"role": "implementor", "status": "no_op", "head": head, "checked": "nothing changed"})},
			wantAction: "dispatch", wantRole: "qa", wantCode: "implementation_completed",
		},
		{
			name:       "implementor blocked to Lead",
			comments:   []supervisor.Comment{eventComment(1, testTime, map[string]any{"role": "implementor", "status": "blocked"})},
			wantAction: "dispatch", wantRole: "lead", wantCode: "lead_first_blocker",
		},
		{
			name: "QA approved to Lead",
			comments: []supervisor.Comment{
				eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": head}),
				eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "qa", "status": "completed", "head": head, "verdict": "approved"}),
			},
			wantAction: "dispatch", wantRole: "lead", wantCode: "qa_approved",
		},
		{
			name:       "QA changes to Implementor",
			comments:   []supervisor.Comment{eventComment(2, testTime, map[string]any{"role": "qa", "status": "completed", "head": head, "verdict": "changes_required"})},
			wantAction: "dispatch", wantRole: "implementor", wantCode: "qa_changes_required",
		},
		{
			name:       "QA inconclusive to Lead",
			comments:   []supervisor.Comment{eventComment(2, testTime, map[string]any{"role": "qa", "status": "completed", "head": head, "verdict": "inconclusive"})},
			wantAction: "dispatch", wantRole: "lead", wantCode: "qa_inconclusive",
		},
		{
			name:       "QA blocked to Lead",
			comments:   []supervisor.Comment{eventComment(2, testTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"})},
			wantAction: "dispatch", wantRole: "lead", wantCode: "lead_first_blocker",
		},
		{
			name:       "Lead correction resumes Implementor",
			comments:   []supervisor.Comment{eventComment(3, testTime, map[string]any{"role": "lead", "status": "completed", "decision": "correct", "resume_role": "implementor"})},
			wantAction: "dispatch", wantRole: "implementor", wantCode: "lead_resume",
		},
		{
			name:       "Lead owner escalation has no worker lane",
			comments:   []supervisor.Comment{eventComment(3, testTime, map[string]any{"role": "lead", "status": "blocked", "decision": "owner_required", "escalate_to": "owner"})},
			wantAction: "owner_attention", wantRole: "", wantCode: "owner_required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := DeriveProject(projectSnapshot("acme", "service", 1, issueWith(head, test.comments...))).WorkUnits[0]
			if state.Route.Action != test.wantAction || state.Route.TargetRole != test.wantRole || state.Route.ReasonCode != test.wantCode {
				t.Fatalf("route = %#v", state.Route)
			}
			if strings.Contains(test.name, "no-op") && state.Route.TargetRole == "implementor" {
				t.Fatal("no-op repeated the identical Implementor route")
			}
			if state.Route.TargetRole != "" {
				wantLane := "acme/service#6:" + state.Route.TargetRole
				if state.Route.LaneKey != wantLane {
					t.Fatalf("lane_key = %q, want %q", state.Route.LaneKey, wantLane)
				}
			}
		})
	}
}

func TestChangedHeadInvalidatesHandoffAndQAApproval(t *testing.T) {
	oldHead := fullHead("a")
	newHead := fullHead("b")
	issue := issueWith(newHead,
		eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": oldHead}),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "qa", "status": "completed", "head": oldHead, "verdict": "approved"}),
	)
	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]

	if state.LatestResults.Implementor == nil || !state.LatestResults.Implementor.Stale || state.LatestResults.Implementor.Effective {
		t.Fatalf("implementor evidence = %#v", state.LatestResults.Implementor)
	}
	if state.LatestResults.QA == nil || !state.LatestResults.QA.Stale || state.QAApprovedHead != "" {
		t.Fatalf("QA state = %#v approved=%q", state.LatestResults.QA, state.QAApprovedHead)
	}
	if state.Route.Action != "manual_attention" || state.Route.TargetRole != "lead" {
		t.Fatalf("route = %#v", state.Route)
	}
	if state.Attention.Kind != AttentionQAInvalidated {
		t.Fatalf("attention = %#v", state.Attention)
	}
}

func TestDuplicateOutOfOrderRecordsAreDeterministic(t *testing.T) {
	head := fullHead("c")
	first := eventComment(10, testTime.Add(time.Minute), map[string]any{"role": "implementor", "status": "completed", "head": head})
	second := eventComment(20, testTime.Add(2*time.Minute), map[string]any{"role": "qa", "status": "completed", "head": head, "verdict": "approved"})
	duplicateOlder := first
	duplicateOlder.UpdatedAt = testTime
	duplicateOlder.Body = "older duplicate"

	leftIssue := issueWith(head, second, duplicateOlder, first)
	rightIssue := issueWith(head, first, second, duplicateOlder)
	left := DeriveProject(projectSnapshot("acme", "service", 1, leftIssue)).WorkUnits[0]
	right := DeriveProject(projectSnapshot("acme", "service", 1, rightIssue)).WorkUnits[0]

	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	if string(leftJSON) != string(rightJSON) {
		t.Fatalf("derived state depends on input order\nleft=%s\nright=%s", leftJSON, rightJSON)
	}
	if len(left.ParsedComments) != 3 {
		t.Fatalf("parsed comments = %d, want 3 including Lead Dispatch", len(left.ParsedComments))
	}
	assertWarning(t, left.Warnings, "duplicate_comment")
}

func TestMissingLabelsAndAmbiguousPRsRemainAnalyzable(t *testing.T) {
	headA := fullHead("a")
	headB := fullHead("b")
	issue := issueWith(headA)
	issue.Labels = nil
	issue.PullRequests = append(issue.PullRequests, supervisor.PullRequest{
		GitHubID: 701, Number: 8, Title: "other", State: "open", Draft: true,
		BaseRef: "main", HeadRef: "other", HeadSHA: headB, URL: "https://example/pr/8", UpdatedAt: testTime,
	})
	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]

	if state.Lifecycle != "unknown" || !state.Candidate.Ambiguous || state.Candidate.Current != nil {
		t.Fatalf("state = %#v", state)
	}
	assertWarning(t, state.Warnings, "missing_lifecycle_label")
	assertWarning(t, state.Warnings, "ambiguous_candidate")
	if state.Attention.Kind != AttentionAmbiguous || state.Route.Action != "manual_attention" {
		t.Fatalf("attention=%#v route=%#v", state.Attention, state.Route)
	}
}

func TestMalformedEnvelopeCannotTriggerDangerousTransition(t *testing.T) {
	head := fullHead("d")
	issue := issueWith(head, comment(1, testTime, `<!-- supervisor:event {"v":1,} -->`))
	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.Route.Action != "manual_attention" || state.Route.TargetRole != "lead" {
		t.Fatalf("route = %#v", state.Route)
	}
	if state.Attention.Kind != AttentionProtocolWarning {
		t.Fatalf("attention = %#v", state.Attention)
	}
}

func TestLeadResolutionClearsWorkerBlocker(t *testing.T) {
	head := fullHead("e")
	issue := issueWith(head,
		eventComment(1, testTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"}),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "lead", "status": "completed", "decision": "continue", "resume_role": "qa", "resolves": 1}),
	)
	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.ActiveBlocker != nil {
		t.Fatalf("active blocker = %#v", state.ActiveBlocker)
	}
	if state.Route.TargetRole != "qa" || state.Route.ReasonCode != "lead_resume" {
		t.Fatalf("route = %#v", state.Route)
	}
}

func TestWorkspaceIsolationAndOrdering(t *testing.T) {
	head := fullHead("f")
	workspace := supervisor.WorkspaceSnapshot{
		GeneratedAt: testTime,
		Projects: []supervisor.ProjectSnapshot{
			projectSnapshot("zeta", "two", 2, issueWith(head)),
			projectSnapshot("alpha", "one", 1, issueWith(head, eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": head}))),
		},
	}
	state := DeriveWorkspace(workspace)
	if len(state.Projects) != 2 || state.Projects[0].Project.Owner != "alpha" || state.Projects[1].Project.Owner != "zeta" {
		t.Fatalf("projects = %#v", state.Projects)
	}
	if state.Projects[0].WorkUnits[0].Identity.ProjectID != 1 || state.Projects[1].WorkUnits[0].Identity.ProjectID != 2 {
		t.Fatalf("project identities leaked: %#v", state.Projects)
	}
	if state.Projects[0].WorkUnits[0].Route.LaneKey == state.Projects[1].WorkUnits[0].Route.LaneKey {
		t.Fatalf("lane keys collide: %q", state.Projects[0].WorkUnits[0].Route.LaneKey)
	}
}

func TestCIFailureAndPendingAttention(t *testing.T) {
	head := fullHead("9")
	failedIssue := issueWith(head, eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": head}))
	failedIssue.PullRequests[0].CI = supervisor.CISummary{HeadSHA: head, Status: "completed", Conclusion: "failure", UpdatedAt: testTime}
	failed := DeriveProject(projectSnapshot("acme", "service", 1, failedIssue)).WorkUnits[0]
	if failed.Attention.Kind != AttentionCIFailed || failed.Route.TargetRole != "implementor" {
		t.Fatalf("failed attention=%#v route=%#v", failed.Attention, failed.Route)
	}

	pendingIssue := issueWith(head, eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": head}))
	pendingIssue.PullRequests[0].CI = supervisor.CISummary{HeadSHA: head, Status: "in_progress", UpdatedAt: testTime}
	pending := DeriveProject(projectSnapshot("acme", "service", 1, pendingIssue)).WorkUnits[0]
	if pending.Attention.Kind != AttentionWaiting || pending.Route.Action != "none" {
		t.Fatalf("pending attention=%#v route=%#v", pending.Attention, pending.Route)
	}
}

func TestFencedContractExamplesAreNotOperationalEvents(t *testing.T) {
	head := fullHead("1")
	dispatchBody := "## Lead Dispatch\n\nUse the following terminal forms:\n\n" +
		"```html\n<!-- supervisor:event\n{\"v\":1,\"event\":\"worker_result\",\"role\":\"implementor\",\"status\":\"completed\",\"head\":\"example\"}\n-->\n```\n\n" +
		"~~~html\n<!-- supervisor:event\n{\"v\":1,\"event\":\"worker_result\",\"role\":\"implementor\",\"status\":\"no_op\",\"head\":\"example\"}\n-->\n~~~\n\n" +
		"> <!-- supervisor:event {\"v\":1,\"event\":\"worker_result\",\"role\":\"qa\",\"status\":\"completed\",\"head\":\"example\",\"verdict\":\"approved\"} -->\n"
	dispatch := comment(90, testTime.Add(-time.Hour), dispatchBody)
	parsed := ParseComment(1, 6, dispatch)
	if parsed.Event != nil || parsed.Level != ParseLevelActivity || !hasMarkdownHeading(parsed.Markdown, "Lead Dispatch") {
		t.Fatalf("dispatch example became operational evidence: %#v", parsed)
	}

	plainText := ParseComment(1, 6, comment(91, testTime, "QA Verdict\nHead: `"+head+"`\nVerdict: approved"))
	if plainText.Event != nil || plainText.Level != ParseLevelActivity {
		t.Fatalf("plain prose became a Markdown heading result: %#v", plainText)
	}

	issue := issueWith(head, eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": head}))
	issue.Comments[0] = dispatch
	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.Route.TargetRole != "qa" || state.Route.ReasonCode != "implementation_completed" {
		t.Fatalf("fenced examples poisoned route: %#v", state.Route)
	}
}

func TestLaterValidResultSupersedesHistoricalMalformedEnvelope(t *testing.T) {
	head := fullHead("2")
	issue := issueWith(head,
		comment(1, testTime, `<!-- supervisor:event {"v":1,} -->`),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "implementor", "status": "completed", "head": head}),
	)
	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.Route.TargetRole != "qa" || state.Route.ReasonCode != "implementation_completed" {
		t.Fatalf("historical malformed evidence permanently blocked route: %#v", state.Route)
	}
	if state.Attention.Kind == AttentionProtocolWarning {
		t.Fatalf("historical malformed evidence permanently retained protocol attention: %#v", state.Attention)
	}
	assertWarning(t, state.Warnings, "malformed_envelope")
}

func TestLeadResolutionMustMatchActiveBlocker(t *testing.T) {
	head := fullHead("3")
	unmatched := issueWith(head,
		eventComment(1, testTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"}),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "lead", "status": "completed", "decision": "continue", "resume_role": "qa", "resolves": 999}),
	)
	state := DeriveProject(projectSnapshot("acme", "service", 1, unmatched)).WorkUnits[0]
	if state.ActiveBlocker == nil || state.ActiveBlocker.CommentID != 1 {
		t.Fatalf("unmatched Lead result cleared blocker: %#v", state.ActiveBlocker)
	}
	if state.Route.Action != "manual_attention" || state.Route.ReasonCode != "unresolved_active_blocker" {
		t.Fatalf("unmatched Lead result routed a worker: %#v", state.Route)
	}
	assertWarning(t, state.Warnings, "unmatched_blocker_resolution")

	matched := issueWith(head,
		eventComment(1, testTime, map[string]any{"role": "qa", "status": "blocked", "head": head, "verdict": "inconclusive"}),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "lead", "status": "completed", "decision": "continue", "resume_role": "qa", "resolves": []any{1}}),
	)
	resolved := DeriveProject(projectSnapshot("acme", "service", 1, matched)).WorkUnits[0]
	if resolved.ActiveBlocker != nil || resolved.Route.TargetRole != "qa" || resolved.Route.ReasonCode != "lead_resume" {
		t.Fatalf("matched Lead resolution did not resume QA: blocker=%#v route=%#v", resolved.ActiveBlocker, resolved.Route)
	}
}

func TestFreshQAVerdictSupersedesInvalidatedApproval(t *testing.T) {
	oldHead := fullHead("4")
	currentHead := fullHead("5")
	issue := issueWith(currentHead,
		eventComment(1, testTime, map[string]any{"role": "qa", "status": "completed", "head": oldHead, "verdict": "approved"}),
		eventComment(2, testTime.Add(time.Minute), map[string]any{"role": "implementor", "status": "completed", "head": currentHead}),
		eventComment(3, testTime.Add(2*time.Minute), map[string]any{"role": "qa", "status": "completed", "head": currentHead, "verdict": "approved"}),
	)
	state := DeriveProject(projectSnapshot("acme", "service", 1, issue)).WorkUnits[0]
	if state.QAApprovedHead != currentHead {
		t.Fatalf("QA approved Head = %q, want %q", state.QAApprovedHead, currentHead)
	}
	if state.Attention.Kind == AttentionQAInvalidated {
		t.Fatalf("fresh QA approval remained invalidated: %#v", state.Attention)
	}
	if state.Route.TargetRole != "lead" || state.Route.ReasonCode != "qa_approved" {
		t.Fatalf("route = %#v", state.Route)
	}
}

func TestCIIsBoundToCurrentHeadAndRequiredBeforeQA(t *testing.T) {
	currentHead := fullHead("6")
	oldHead := fullHead("7")

	missing := issueWith(currentHead, eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": currentHead}))
	missing.PullRequests[0].CI = supervisor.CISummary{}
	missingState := DeriveProject(projectSnapshot("acme", "service", 1, missing)).WorkUnits[0]
	if missingState.Route.Action != "none" || missingState.Route.ReasonCode != "waiting_for_ci" || missingState.Attention.Kind != AttentionWaiting {
		t.Fatalf("missing CI advanced workflow: attention=%#v route=%#v", missingState.Attention, missingState.Route)
	}

	stale := issueWith(currentHead, eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": currentHead}))
	stale.PullRequests[0].CI = supervisor.CISummary{HeadSHA: oldHead, Status: "completed", Conclusion: "failure", UpdatedAt: testTime}
	staleState := DeriveProject(projectSnapshot("acme", "service", 1, stale)).WorkUnits[0]
	if staleState.CI.HeadSHA != "" || staleState.Route.TargetRole == "implementor" || staleState.Route.ReasonCode != "waiting_for_ci" {
		t.Fatalf("stale CI affected routing: ci=%#v route=%#v", staleState.CI, staleState.Route)
	}
	assertWarning(t, staleState.Warnings, "stale_ci_summary")

	exact := issueWith(currentHead, eventComment(1, testTime, map[string]any{"role": "implementor", "status": "completed", "head": currentHead}))
	exactState := DeriveProject(projectSnapshot("acme", "service", 1, exact)).WorkUnits[0]
	if exactState.Route.TargetRole != "qa" || exactState.Route.ReasonCode != "implementation_completed" {
		t.Fatalf("exact successful CI did not advance: %#v", exactState.Route)
	}
}

func projectSnapshot(owner, repository string, id int64, issues ...supervisor.Issue) supervisor.ProjectSnapshot {
	return supervisor.ProjectSnapshot{
		Project: supervisor.Project{ID: id, Owner: owner, Repository: repository, WorkflowMode: "pull_request"},
		Issues:  issues,
	}
}

func issueWith(head string, comments ...supervisor.Comment) supervisor.Issue {
	return supervisor.Issue{
		GitHubID: 600, Number: 6, Title: "Stage 3", State: "open", URL: "https://example/issues/6",
		CreatedAt: testTime, UpdatedAt: testTime,
		Labels:   []supervisor.Label{{Name: "implementation"}},
		Comments: append([]supervisor.Comment{comment(99, testTime.Add(-time.Hour), "## Lead Dispatch\n\nImplement the issue.")}, comments...),
		PullRequests: []supervisor.PullRequest{{
			GitHubID: 700, Number: 7, Title: "Stage 3", State: "open", Draft: true,
			BaseRef: "main", HeadRef: "stage-3", HeadSHA: head, URL: "https://example/pr/7", UpdatedAt: testTime,
			CI: supervisor.CISummary{HeadSHA: head, Status: "completed", Conclusion: "success", UpdatedAt: testTime},
		}},
	}
}

func eventComment(id int64, at time.Time, fields map[string]any) supervisor.Comment {
	payload := map[string]any{"v": 1, "event": "worker_result"}
	for key, value := range fields {
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return comment(id, at, "<!-- supervisor:event\n"+string(encoded)+"\n-->")
}

func comment(id int64, at time.Time, body string) supervisor.Comment {
	return supervisor.Comment{GitHubID: id, Body: body, Author: "worker", URL: "https://example/comments", CreatedAt: at, UpdatedAt: at}
}

func fullHead(character string) string {
	return strings.Repeat(character, 40)
}

func assertWarning(t *testing.T, warnings []Warning, code string) {
	t.Helper()
	for _, item := range warnings {
		if item.Code == code {
			return
		}
	}
	t.Fatalf("warning %q not found in %#v", code, warnings)
}

func TestFindWorkUnit(t *testing.T) {
	project := ProjectState{WorkUnits: []WorkUnitState{
		{Identity: WorkUnitIdentity{IssueNumber: 2}},
		{Identity: WorkUnitIdentity{IssueNumber: 6}},
	}}
	got, ok := FindWorkUnit(project, 6)
	if !ok || !reflect.DeepEqual(got.Identity, WorkUnitIdentity{IssueNumber: 6}) {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
	if _, ok := FindWorkUnit(project, 3); ok {
		t.Fatal("unexpected work unit")
	}
}
