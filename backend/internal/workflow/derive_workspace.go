package workflow

import (
	"sort"
	"strings"

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
	results, commands := projectExternal(snapshot.Project.ID)
	return deriveProjectWithExternalState(snapshot, results, commands)
}

func DeriveProjectWithExternal(snapshot supervisor.ProjectSnapshot, external map[int64]ExternalResult) ProjectState {
	return deriveProjectWithExternalState(snapshot, external, nil)
}

func deriveProjectWithExternalState(snapshot supervisor.ProjectSnapshot, external map[int64]ExternalResult, commands map[int]ExternalCommand) ProjectState {
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
		var command *ExternalCommand
		if value, ok := commands[issue.Number]; ok {
			copy := value
			command = &copy
		}
		workUnit := deriveWorkUnit(identity, snapshot.Project.WorkflowMode, issue, external, command)
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

func deriveWorkUnit(project ProjectIdentity, workflowMode string, issue supervisor.Issue, external map[int64]ExternalResult, command *ExternalCommand) WorkUnitState {
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
		if overlay, ok := external[comment.GitHubID]; ok {
			applyExternalResult(&parsed, overlay)
		}
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
		_, commandBound := parsed.Event.Extensions["command_id"]
		if !dispatchSeen && !commandBound {
			evidence.Warnings = append(evidence.Warnings, warning(evidence.CommentID, "missing_dispatch_correlation", "no earlier Lead Dispatch comment or Dashboard command can be correlated with this terminal result"))
		}
		candidateBound := !commandBound || parsed.Event.Role != "implementor" || (parsed.Event.Status != "continue" && parsed.Event.Status != "no_op")
		if candidateBound {
			correlateEvidence(&evidence, state.Candidate, state.CurrentHead)
		}
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
	state.Route = routeWithExternalCommand(project, state, state.Route, command)
	state.Attention = deriveAttention(issue.State, state)
	return state
}

func routeWithExternalCommand(project ProjectIdentity, state WorkUnitState, route Route, command *ExternalCommand) Route {
	if command == nil {
		return route
	}
	switch command.Status {
	case "created", "delivery_pending":
		return Route{
			Action: "none", ReasonCode: "workflow_delivery_pending",
			Reason:       "a Dashboard workflow command already exists and is awaiting confirmed browser delivery",
			ExpectedHead: command.ExpectedHead, Guards: append(guardsForHead(command.ExpectedHead), "active_workflow_command", "no_duplicate_dispatch"), Warnings: state.Warnings,
		}
	case "awaiting_result":
		return Route{
			Action: "none", ReasonCode: "awaiting_worker_result",
			Reason:       "the prompt was delivered; execution remains open until a correlated GitHub worker-result marker is accepted",
			ExpectedHead: command.ExpectedHead, Guards: append(guardsForHead(command.ExpectedHead), "active_workflow_command", "github_result_required", "no_duplicate_dispatch"), Warnings: state.Warnings,
		}
	case "ambiguous":
		return manualLeadRoute(project, state, "workflow_command_ambiguous", "workflow execution has conflicting or uncertain terminal evidence")
	case "failed":
		return manualLeadRoute(project, state, "workflow_command_failed", "workflow command delivery or execution failed and requires explicit Lead review")
	default:
		return route
	}
}

func applyExternalResult(parsed *ParsedComment, external ExternalResult) {
	parsed.Level = ParseLevelEnvelope
	parsed.Meaningful = true
	parsed.TransitionSafe = external.TransitionSafe
	parsed.Event = external.Event
	parsed.HardError = external.HardError
	parsed.Warnings = append(parsed.Warnings[:0], external.Warnings...)
	if parsed.HardError != nil {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, parsed.HardError.Code, parsed.HardError.Message))
	}
}
