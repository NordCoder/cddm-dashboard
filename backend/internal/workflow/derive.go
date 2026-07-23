package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func DeriveWorkspace(snapshot supervisor.WorkspaceSnapshot) WorkspaceState {
	projects := append([]supervisor.ProjectSnapshot(nil), snapshot.Projects...)
	sort.Slice(projects, func(i, j int) bool {
		left, right := projects[i].Project, projects[j].Project
		if strings.ToLower(left.Owner) != strings.ToLower(right.Owner) {
			return strings.ToLower(left.Owner) < strings.ToLower(right.Owner)
		}
		if strings.ToLower(left.Repository) != strings.ToLower(right.Repository) {
			return strings.ToLower(left.Repository) < strings.ToLower(right.Repository)
		}
		return left.ID < right.ID
	})

	state := WorkspaceState{
		GeneratedAt: snapshot.GeneratedAt,
		Projects:    make([]ProjectState, 0, len(projects)),
		Attention:   make([]AttentionItem, 0),
	}
	for _, project := range projects {
		projectState := DeriveProject(project)
		state.Projects = append(state.Projects, projectState)
		state.Attention = append(state.Attention, projectState.Attention...)
	}
	sortAttention(state.Attention)
	return state
}

func DeriveProject(snapshot supervisor.ProjectSnapshot) ProjectState {
	identity := ProjectIdentity{
		ID:           snapshot.Project.ID,
		Owner:        snapshot.Project.Owner,
		Repository:   snapshot.Project.Repository,
		WorkflowMode: snapshot.Project.WorkflowMode,
	}
	issues := append([]supervisor.Issue(nil), snapshot.Issues...)
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Number != issues[j].Number {
			return issues[i].Number < issues[j].Number
		}
		return issues[i].GitHubID < issues[j].GitHubID
	})

	state := ProjectState{
		Project:   identity,
		WorkUnits: make([]WorkUnitState, 0, len(issues)),
		Attention: make([]AttentionItem, 0),
	}
	for _, issue := range issues {
		workUnit := deriveWorkUnit(identity, snapshot.Project.WorkflowMode, issue)
		state.WorkUnits = append(state.WorkUnits, workUnit)
		if workUnit.Attention.Kind != AttentionNormal && workUnit.Attention.Kind != AttentionTerminal {
			state.Attention = append(state.Attention, AttentionItem{
				Project: identity, WorkUnit: workUnit.Identity, Attention: workUnit.Attention, Route: workUnit.Route,
			})
		}
	}
	sortAttention(state.Attention)
	return state
}

func FindWorkUnit(project ProjectState, issueNumber int) (WorkUnitState, bool) {
	index := sort.Search(len(project.WorkUnits), func(index int) bool {
		return project.WorkUnits[index].Identity.IssueNumber >= issueNumber
	})
	if index < len(project.WorkUnits) && project.WorkUnits[index].Identity.IssueNumber == issueNumber {
		return project.WorkUnits[index], true
	}
	return WorkUnitState{}, false
}

func deriveWorkUnit(project ProjectIdentity, workflowMode string, issue supervisor.Issue) WorkUnitState {
	state := WorkUnitState{
		Identity: WorkUnitIdentity{
			ProjectID: project.ID, Owner: project.Owner, Repository: project.Repository,
			IssueGitHubID: issue.GitHubID, IssueNumber: issue.Number, Title: issue.Title, URL: issue.URL,
		},
		Lifecycle:      "unknown",
		Candidate:      CandidateState{Alternatives: make([]Candidate, 0)},
		ParsedComments: make([]ParsedComment, 0),
		Warnings:       make([]Warning, 0),
		CI:             supervisor.CISummary{},
	}

	state.Lifecycle, state.Warnings = deriveLifecycle(issue.Labels, state.Warnings)
	state.Candidate, state.Warnings = deriveCandidate(issue.PullRequests, state.Warnings)
	if state.Candidate.Current != nil {
		state.CurrentHead = state.Candidate.Current.HeadSHA
		state.CI = state.Candidate.Current.CI
	}

	comments, duplicateWarnings := canonicalComments(issue.Comments)
	state.Warnings = append(state.Warnings, duplicateWarnings...)
	for _, comment := range comments {
		parsed := ParseComment(project.ID, issue.Number, comment)
		state.ParsedComments = append(state.ParsedComments, parsed)
		state.Warnings = append(state.Warnings, parsed.Warnings...)
	}

	state.LastMeaningfulActivity = issue.UpdatedAt
	for _, pr := range issue.PullRequests {
		state.LastMeaningfulActivity = latestTime(state.LastMeaningfulActivity, pr.UpdatedAt)
	}
	for _, parsed := range state.ParsedComments {
		if parsed.Meaningful {
			state.LastMeaningfulActivity = latestTime(state.LastMeaningfulActivity, latestTime(parsed.CreatedAt, parsed.UpdatedAt))
		}
	}

	results := make([]ResultEvidence, 0)
	dispatchSeen := false
	for index := range state.ParsedComments {
		parsed := &state.ParsedComments[index]
		if hasMarkdownHeading(parsed.Markdown, "Lead Dispatch") {
			dispatchSeen = true
		}
		if parsed.Event == nil {
			continue
		}
		evidence := evidenceFromParsed(*parsed)
		if !dispatchSeen {
			evidence.Warnings = append(evidence.Warnings, warning(evidence.CommentID, "missing_dispatch_correlation", "no earlier Lead Dispatch comment can be correlated with this terminal result"))
		}
		correlateEvidence(&evidence, state.Candidate, state.CurrentHead)
		parsed.Warnings = append(parsed.Warnings, evidence.Warnings...)
		results = append(results, evidence)
		state.Warnings = append(state.Warnings, evidence.Warnings...)
		assignLatest(&state.LatestResults, evidence)
	}

	state.ActiveBlocker = activeBlocker(results)
	if state.LatestResults.QA != nil {
		state.QAReviewedHead = state.LatestResults.QA.Head
		if state.LatestResults.QA.Verdict == "approved" && state.LatestResults.QA.Effective {
			state.QAApprovedHead = state.LatestResults.QA.Head
		}
	}

	state.Warnings = stableWarnings(state.Warnings)
	state.Route = deriveRoute(project, workflowMode, issue.State, state, results)
	state.Attention = deriveAttention(issue.State, state)
	return state
}

func deriveLifecycle(labels []supervisor.Label, warnings []Warning) (string, []Warning) {
	found := make(map[string]struct{})
	for _, label := range labels {
		value := normalizeLifecycleLabel(label.Name)
		if value != "" {
			found[value] = struct{}{}
		}
	}
	if len(found) == 0 {
		return "unknown", append(warnings, Warning{Code: "missing_lifecycle_label", Message: "no canonical lifecycle label is present; other workflow signals remain analyzable"})
	}
	values := make([]string, 0, len(found))
	for value := range found {
		values = append(values, value)
	}
	sort.Strings(values)
	if len(values) > 1 {
		return "unknown", append(warnings, Warning{Code: "ambiguous_lifecycle_label", Message: "multiple lifecycle labels are present: " + strings.Join(values, ", ")})
	}
	return values[0], warnings
}

func normalizeLifecycleLabel(label string) string {
	value := normalizeToken(label)
	for _, prefix := range []string{"status_", "lifecycle_", "stage_"} {
		value = strings.TrimPrefix(value, prefix)
	}
	switch value {
	case "backlog":
		return "backlog"
	case "ready", "ready_for_work":
		return "ready"
	case "implementation", "implementing", "in_progress":
		return "implementation"
	case "qa", "quality_assurance", "review":
		return "qa"
	case "blocked":
		return "blocked"
	case "done", "completed", "terminal", "merged":
		return "terminal"
	default:
		return ""
	}
}

func deriveCandidate(pullRequests []supervisor.PullRequest, warnings []Warning) (CandidateState, []Warning) {
	open := make([]Candidate, 0)
	for _, pr := range pullRequests {
		if strings.EqualFold(pr.State, "open") {
			open = append(open, candidateFromSnapshot(pr))
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].Number < open[j].Number })
	state := CandidateState{Alternatives: open}
	switch len(open) {
	case 0:
		return state, warnings
	case 1:
		current := open[0]
		state.Current = &current
		return state, warnings
	default:
		state.Ambiguous = true
		warnings = append(warnings, Warning{Code: "ambiguous_candidate", Message: fmt.Sprintf("%d open pull requests are linked; no current Candidate was selected", len(open))})
		return state, warnings
	}
}

func candidateFromSnapshot(pr supervisor.PullRequest) Candidate {
	return Candidate{
		GitHubID: pr.GitHubID, Number: pr.Number, Title: pr.Title, Draft: pr.Draft,
		MergeableState: pr.MergeableState, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef,
		HeadSHA: pr.HeadSHA, URL: pr.URL, CI: pr.CI,
	}
}

func canonicalComments(comments []supervisor.Comment) ([]supervisor.Comment, []Warning) {
	byID := make(map[int64]supervisor.Comment, len(comments))
	duplicates := make(map[int64]struct{})
	for _, comment := range comments {
		if existing, ok := byID[comment.GitHubID]; ok {
			duplicates[comment.GitHubID] = struct{}{}
			if commentPreferred(comment, existing) {
				byID[comment.GitHubID] = comment
			}
			continue
		}
		byID[comment.GitHubID] = comment
	}
	result := make([]supervisor.Comment, 0, len(byID))
	for _, comment := range byID {
		result = append(result, comment)
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		if result[i].GitHubID != result[j].GitHubID {
			return result[i].GitHubID < result[j].GitHubID
		}
		if !result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].UpdatedAt.Before(result[j].UpdatedAt)
		}
		return result[i].Body < result[j].Body
	})
	warnings := make([]Warning, 0, len(duplicates))
	ids := make([]int64, 0, len(duplicates))
	for id := range duplicates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		warnings = append(warnings, warning(id, "duplicate_comment", "duplicate records with the same stable GitHub comment ID were collapsed deterministically"))
	}
	return result, warnings
}

func commentPreferred(candidate, existing supervisor.Comment) bool {
	if !candidate.UpdatedAt.Equal(existing.UpdatedAt) {
		return candidate.UpdatedAt.After(existing.UpdatedAt)
	}
	if !candidate.CreatedAt.Equal(existing.CreatedAt) {
		return candidate.CreatedAt.After(existing.CreatedAt)
	}
	if candidate.Body != existing.Body {
		return candidate.Body > existing.Body
	}
	return candidate.URL > existing.URL
}

func evidenceFromParsed(parsed ParsedComment) ResultEvidence {
	event := parsed.Event
	return ResultEvidence{
		ProjectID: parsed.ProjectID, IssueNumber: parsed.IssueNumber, CommentID: parsed.CommentID,
		Role: event.Role, Status: event.Status, Head: event.Head, Verdict: event.Verdict,
		Decision: event.Decision, ResumeRole: event.ResumeRole, Resolves: event.Resolves, EscalateTo: event.EscalateTo,
		Level: parsed.Level, Effective: parsed.TransitionSafe && parsed.HardError == nil,
		CreatedAt: parsed.CreatedAt, Warnings: make([]Warning, 0), Extensions: event.Extensions,
	}
}

func correlateEvidence(evidence *ResultEvidence, candidate CandidateState, currentHead string) {
	if candidate.Ambiguous && evidence.Status != "blocked" {
		evidence.Effective = false
		evidence.Warnings = append(evidence.Warnings, warning(evidence.CommentID, "ambiguous_candidate_correlation", "terminal result cannot be correlated while multiple open Candidates exist"))
	}

	candidateBound := evidence.Role == "qa" || (evidence.Role == "implementor" && evidence.Status != "blocked")
	if !candidateBound {
		return
	}
	if currentHead == "" {
		evidence.Effective = false
		evidence.Warnings = append(evidence.Warnings, warning(evidence.CommentID, "missing_current_head", "candidate-bound result has no uniquely selected current PR Head"))
		return
	}
	if evidence.Head == "" {
		evidence.Effective = false
		evidence.Warnings = append(evidence.Warnings, warning(evidence.CommentID, "missing_candidate_head", "candidate-bound result is missing exact Head evidence"))
		return
	}
	if evidence.Head == currentHead {
		return
	}
	if strings.HasPrefix(currentHead, evidence.Head) || strings.HasPrefix(evidence.Head, currentHead) {
		evidence.Effective = false
		evidence.Warnings = append(evidence.Warnings, warning(evidence.CommentID, "unresolved_head_prefix", "Head prefixes are not accepted as exact Candidate identity"))
		return
	}
	evidence.Stale = true
	evidence.Effective = false
	evidence.Warnings = append(evidence.Warnings, warning(evidence.CommentID, "stale_head", fmt.Sprintf("result Head %s does not match current Head %s", evidence.Head, currentHead)))
}

func assignLatest(results *LatestResults, evidence ResultEvidence) {
	copy := evidence
	switch evidence.Role {
	case "lead":
		results.Lead = &copy
	case "implementor":
		results.Implementor = &copy
	case "qa":
		results.QA = &copy
	}
}

func activeBlocker(results []ResultEvidence) *ResultEvidence {
	var blocker *ResultEvidence
	for _, result := range results {
		copy := result
		if result.Status == "blocked" {
			blocker = &copy
			continue
		}
		if result.Role == "lead" && (result.Decision != "" || result.ResumeRole != "" || len(result.Resolves) > 0 || result.EscalateTo != "") {
			blocker = nil
		}
	}
	return blocker
}

func deriveRoute(project ProjectIdentity, workflowMode, issueState string, state WorkUnitState, results []ResultEvidence) Route {
	route := Route{
		Action: "none", ReasonCode: "waiting_for_terminal_result", Reason: "no safe terminal result currently advances the workflow",
		ExpectedHead: state.CurrentHead, Guards: make([]string, 0), Warnings: state.Warnings,
	}
	if state.CurrentHead != "" {
		route.Guards = append(route.Guards, "exact_head")
	}
	if strings.EqualFold(issueState, "closed") || state.Lifecycle == "terminal" {
		route.ReasonCode = "terminal_work_unit"
		route.Reason = "work unit is terminal"
		return route
	}
	if state.Candidate.Ambiguous {
		return manualLeadRoute(project, state, "ambiguous_candidate", "multiple open Candidates require Lead/manual selection")
	}
	if hasUnsafeTerminalEvidence(state) {
		return manualLeadRoute(project, state, "unsafe_terminal_result", "malformed or incomplete terminal evidence requires Lead/manual review")
	}

	latest := latestResult(results)
	if latest == nil {
		return dispatchRoute(project, state, "implementor", "initial_implementation", "no terminal worker result exists; implementation is the next role")
	}
	if !latest.Effective {
		return manualLeadRoute(project, state, "unsafe_terminal_result", "latest terminal result is malformed, stale, incomplete or otherwise unsafe for automatic transition")
	}
	if latest.Stale {
		return manualLeadRoute(project, state, "stale_terminal_result", "latest terminal result is bound to a non-current Head")
	}

	if latest.Status == "blocked" {
		if latest.Role == "lead" && (latest.EscalateTo == "owner" || latest.Decision == "owner_required") {
			return Route{Action: "owner_attention", ReasonCode: "owner_required", Reason: "Lead escalated the blocker to Owner", ExpectedHead: state.CurrentHead, Guards: []string{"lead_first_blocker_flow"}, Warnings: state.Warnings}
		}
		return dispatchRoute(project, state, "lead", "lead_first_blocker", "blocked Implementor or QA result must be resolved by Lead first")
	}

	if ciFailed(state.CI) && latest.Role == "implementor" {
		return dispatchRoute(project, state, "implementor", "candidate_ci_failed", "current Candidate CI failed and requires Implementor correction")
	}
	if ciPending(state.CI) && latest.Role == "implementor" {
		route.ReasonCode = "waiting_for_ci"
		route.Reason = "current Candidate CI has not reached a terminal successful conclusion"
		return route
	}

	switch latest.Role {
	case "implementor":
		if latest.Status == "completed" || latest.Status == "no_op" {
			if qaRequired(workflowMode) {
				return dispatchRoute(project, state, "qa", "implementation_completed", "Implementor terminal result advances to independent QA")
			}
			return dispatchRoute(project, state, "lead", "implementation_completed_no_qa", "Implementor terminal result advances to Lead because QA is not required")
		}
	case "qa":
		switch latest.Verdict {
		case "approved":
			return dispatchRoute(project, state, "lead", "qa_approved", "QA approved the current exact Head")
		case "changes_required":
			return dispatchRoute(project, state, "implementor", "qa_changes_required", "QA requested changes on the current exact Head")
		case "inconclusive":
			return dispatchRoute(project, state, "lead", "qa_inconclusive", "QA could not reach a conclusive verdict")
		}
	case "lead":
		if latest.EscalateTo == "owner" || latest.Decision == "owner_required" {
			return Route{Action: "owner_attention", ReasonCode: "owner_required", Reason: "Lead requires an Owner decision", ExpectedHead: state.CurrentHead, Guards: []string{"no_worker_dispatch"}, Warnings: state.Warnings}
		}
		if latest.ResumeRole != "" && oneOf(latest.Decision, "continue", "correct", "resume", "") {
			return dispatchRoute(project, state, latest.ResumeRole, "lead_resume", "Lead decision resumes the validated worker role")
		}
	}
	return manualLeadRoute(project, state, "unhandled_terminal_result", "terminal result is preserved but has no safe automatic route")
}

func dispatchRoute(project ProjectIdentity, state WorkUnitState, role, code, reason string) Route {
	return Route{
		Action: "dispatch", TargetRole: role, LaneKey: laneKey(project, state.Identity.IssueNumber, role),
		ReasonCode: code, Reason: reason, ExpectedHead: state.CurrentHead,
		Guards: guardsForHead(state.CurrentHead), Warnings: state.Warnings,
	}
}

func manualLeadRoute(project ProjectIdentity, state WorkUnitState, code, reason string) Route {
	return Route{
		Action: "manual_attention", TargetRole: "lead", LaneKey: laneKey(project, state.Identity.IssueNumber, "lead"),
		ReasonCode: code, Reason: reason, ExpectedHead: state.CurrentHead,
		Guards: append(guardsForHead(state.CurrentHead), "manual_confirmation"), Warnings: state.Warnings,
	}
}

func laneKey(project ProjectIdentity, issueNumber int, role string) string {
	return fmt.Sprintf("%s/%s#%d:%s", strings.ToLower(project.Owner), strings.ToLower(project.Repository), issueNumber, strings.ToLower(role))
}

func guardsForHead(head string) []string {
	if head == "" {
		return []string{}
	}
	return []string{"exact_head"}
}

func qaRequired(workflowMode string) bool {
	value := normalizeToken(workflowMode)
	return value != "no_qa" && value != "lead_only"
}

func latestResult(results []ResultEvidence) *ResultEvidence {
	if len(results) == 0 {
		return nil
	}
	copy := results[len(results)-1]
	return &copy
}

func ciFailed(ci supervisor.CISummary) bool {
	conclusion := normalizeToken(ci.Conclusion)
	return oneOf(conclusion, "failure", "failed", "cancelled", "timed_out", "action_required", "startup_failure")
}

func ciPending(ci supervisor.CISummary) bool {
	if ci.HeadSHA == "" && ci.Status == "" && ci.Conclusion == "" {
		return false
	}
	status := normalizeToken(ci.Status)
	conclusion := normalizeToken(ci.Conclusion)
	return conclusion == "" || oneOf(status, "queued", "in_progress", "pending")
}

func hasUnsafeTerminalEvidence(state WorkUnitState) bool {
	for _, parsed := range state.ParsedComments {
		if parsed.Level == ParseLevelEnvelope && (parsed.HardError != nil || !parsed.TransitionSafe) {
			return true
		}
	}
	return false
}

func deriveAttention(issueState string, state WorkUnitState) Attention {
	if strings.EqualFold(issueState, "closed") || state.Lifecycle == "terminal" {
		return Attention{Kind: AttentionTerminal, Code: "terminal", Explanation: "work unit is terminal"}
	}
	if state.Candidate.Ambiguous {
		return Attention{Kind: AttentionAmbiguous, Code: "ambiguous_candidate", Explanation: "multiple open Candidates prevent safe automatic routing"}
	}
	if state.Route.Action == "owner_attention" {
		return Attention{Kind: AttentionOwnerRequired, Code: "owner_required", Explanation: state.Route.Reason}
	}
	if state.ActiveBlocker != nil {
		return Attention{Kind: AttentionBlocked, Code: "active_blocker", Explanation: "a terminal blocked result remains unresolved by Lead"}
	}
	if ciFailed(state.CI) {
		return Attention{Kind: AttentionCIFailed, Code: "ci_failed", Explanation: "CI failed for the current exact Head"}
	}
	if qaApprovalInvalidated(state) {
		return Attention{Kind: AttentionQAInvalidated, Code: "qa_invalidated", Explanation: "a prior QA approval is stale because the current Head changed"}
	}
	if hasProtocolProblem(state) {
		return Attention{Kind: AttentionProtocolWarning, Code: "protocol_warning", Explanation: "workflow evidence contains malformed, legacy, unknown or incomplete protocol data"}
	}
	if state.Route.ReasonCode == "waiting_for_ci" {
		return Attention{Kind: AttentionWaiting, Code: "waiting_for_ci", Explanation: state.Route.Reason}
	}
	if state.Route.Action == "dispatch" || state.Route.Action == "manual_attention" {
		return Attention{Kind: AttentionActionRequired, Code: state.Route.ReasonCode, Explanation: state.Route.Reason}
	}
	return Attention{Kind: AttentionNormal, Code: "normal", Explanation: "no exceptional attention is required"}
}

func qaApprovalInvalidated(state WorkUnitState) bool {
	for _, parsed := range state.ParsedComments {
		if parsed.Event == nil || parsed.Event.Role != "qa" || parsed.Event.Verdict != "approved" {
			continue
		}
		if parsed.Event.Head != "" && state.CurrentHead != "" && parsed.Event.Head != state.CurrentHead {
			return true
		}
	}
	return false
}

func hasProtocolProblem(state WorkUnitState) bool {
	for _, parsed := range state.ParsedComments {
		if parsed.HardError != nil || parsed.Level == ParseLevelHeading || !parsed.TransitionSafe && parsed.Event != nil {
			return true
		}
	}
	for _, warning := range state.Warnings {
		if oneOf(warning.Code, "unknown_version", "unknown_event", "unknown_role", "unknown_status", "unknown_verdict", "unresolved_head_prefix", "missing_candidate_head", "missing_current_head", "missing_dispatch_correlation", "role_mismatch") {
			return true
		}
	}
	return false
}

func stableWarnings(warnings []Warning) []Warning {
	seen := make(map[string]struct{}, len(warnings))
	result := make([]Warning, 0, len(warnings))
	for _, item := range warnings {
		key := fmt.Sprintf("%d\x00%s\x00%s", item.CommentID, item.Code, item.Message)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CommentID != result[j].CommentID {
			return result[i].CommentID < result[j].CommentID
		}
		if result[i].Code != result[j].Code {
			return result[i].Code < result[j].Code
		}
		return result[i].Message < result[j].Message
	})
	return result
}

func sortAttention(items []AttentionItem) {
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if strings.ToLower(left.Project.Owner) != strings.ToLower(right.Project.Owner) {
			return strings.ToLower(left.Project.Owner) < strings.ToLower(right.Project.Owner)
		}
		if strings.ToLower(left.Project.Repository) != strings.ToLower(right.Project.Repository) {
			return strings.ToLower(left.Project.Repository) < strings.ToLower(right.Project.Repository)
		}
		if left.WorkUnit.IssueNumber != right.WorkUnit.IssueNumber {
			return left.WorkUnit.IssueNumber < right.WorkUnit.IssueNumber
		}
		return left.Attention.Code < right.Attention.Code
	})
}

func latestTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
