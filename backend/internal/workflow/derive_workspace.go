package workflow

import (
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"sort"
	"strings"
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
		state.CI, state.Warnings = effectiveCI(state.Candidate.Current.CI, state.CurrentHead, state.Warnings)
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

	var blockerWarnings []Warning
	state.ActiveBlocker, blockerWarnings = activeBlocker(results)
	state.Warnings = append(state.Warnings, blockerWarnings...)
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
