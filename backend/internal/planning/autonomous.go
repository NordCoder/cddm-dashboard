package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const AutonomousIntentExtension = "autonomous_intent"

type AutonomousIntentDetail struct {
	Action          string `json:"action"`
	Repository      string `json:"repository"`
	Role            string `json:"role"`
	ExpectedHead    string `json:"expected_head,omitempty"`
	PRNumber        int    `json:"pr,omitempty"`
	ExpectedBaseRef string `json:"expected_base_ref,omitempty"`
	WaveID          string `json:"wave_id,omitempty"`
	ControlIssue    int    `json:"control_issue,omitempty"`
}

// GenerateAutonomous preserves the M12-C1 dispatch/correction entrypoint.
func (s *Service) GenerateAutonomous(ctx context.Context, projectID int64, issueNumber int, expectedRole, expectedHead string) (GenerationResult, error) {
	return s.GenerateAutonomousIntent(ctx, projectID, issueNumber, "dispatch", expectedRole, expectedHead)
}

// GenerateAutonomousIntent persists an internally rendered Prompt Plan for the
// exact current deterministic route and one typed Workflow Intent. Caller input
// may constrain typed identity but cannot supply prompt text or replace current
// GitHub routing authority.
func (s *Service) GenerateAutonomousIntent(ctx context.Context, projectID int64, issueNumber int, intentAction, expectedRole, expectedHead string, supplied ...AutonomousIntentDetail) (GenerationResult, error) {
	contextValue, contextJSON, err := s.freshContext(ctx, projectID, issueNumber)
	if err != nil {
		return GenerationResult{}, err
	}
	intentAction = strings.TrimSpace(intentAction)
	expectedRole = strings.TrimSpace(expectedRole)
	expectedHead = strings.TrimSpace(expectedHead)
	if !autonomousIntentMatchesRoute(contextValue, intentAction, expectedRole, expectedHead) {
		return GenerationResult{}, fmt.Errorf("autonomous route no longer matches the claimed Intent")
	}
	detail, err := normalizeAutonomousIntentDetail(contextValue, intentAction, expectedRole, expectedHead, supplied)
	if err != nil {
		return GenerationResult{}, err
	}

	createdAt := s.now().UTC()
	plan := RenderFallback(contextValue)
	plan.Summary = autonomousSummary(intentAction)
	plan.Reason = contextValue.Route.Reason
	plan.Risk = "Autonomous materialization remains bounded by the typed Intent, exact current route, Candidate Head, provisioned role session and command-bound result protocol."
	plan.Prompt = autonomousPrompt(contextValue, intentAction, detail)
	if plan.Extensions == nil {
		plan.Extensions = make(map[string]json.RawMessage)
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return GenerationResult{}, err
	}
	plan.Extensions[AutonomousIntentExtension] = detailJSON
	planJSON, err := CanonicalPlanBytes(plan)
	if err != nil {
		return GenerationResult{}, err
	}
	decision := PolicyDecision{
		Status: StatusApproved, ContextHash: contextValue.ContextHash,
		PlanHash: hashBytes(planJSON), DecidedAt: createdAt,
	}
	id, err := s.audit.Save(ctx, GenerationRecord{
		ProjectID: projectID, IssueNumber: issueNumber, Mode: ModeFallback, Status: StatusFallback,
		Context: contextValue, ContextJSON: contextJSON, Plan: &plan, PlanJSON: planJSON,
		Decision: decision, CreatedAt: createdAt,
	})
	if err != nil {
		return GenerationResult{}, err
	}
	return GenerationResult{
		Status: StatusFallback, Context: contextValue, Plan: &plan,
		PolicyDecision: decision, PlanID: id, CreatedAt: createdAt,
	}, nil
}

func normalizeAutonomousIntentDetail(value PromptContext, action, role, head string, supplied []AutonomousIntentDetail) (AutonomousIntentDetail, error) {
	if len(supplied) > 1 {
		return AutonomousIntentDetail{}, fmt.Errorf("one autonomous Intent detail is allowed")
	}
	detail := AutonomousIntentDetail{
		Action: action, Repository: value.Repository.Owner + "/" + value.Repository.Repository,
		Role: role, ExpectedHead: head,
	}
	if len(supplied) == 1 {
		detail = supplied[0]
	}
	detail.Action = strings.TrimSpace(detail.Action)
	detail.Repository = strings.TrimSpace(detail.Repository)
	detail.Role = strings.TrimSpace(detail.Role)
	detail.ExpectedHead = strings.TrimSpace(detail.ExpectedHead)
	detail.ExpectedBaseRef = strings.TrimSpace(detail.ExpectedBaseRef)
	detail.WaveID = strings.TrimSpace(detail.WaveID)
	expectedRepository := value.Repository.Owner + "/" + value.Repository.Repository
	if detail.Action != action || !strings.EqualFold(detail.Repository, expectedRepository) || detail.Role != role || detail.ExpectedHead != head {
		return AutonomousIntentDetail{}, fmt.Errorf("autonomous Intent detail conflicts with current route identity")
	}
	switch action {
	case "merge_candidate":
		if detail.PRNumber <= 0 || detail.ExpectedBaseRef == "" || head == "" {
			return AutonomousIntentDetail{}, fmt.Errorf("merge Intent detail requires PR, base branch and exact Head")
		}
	case "plan_next_wave":
		if detail.WaveID == "" || detail.ControlIssue != value.Issue.Number {
			return AutonomousIntentDetail{}, fmt.Errorf("next-Wave Intent detail requires prior Wave and current Control Issue")
		}
	}
	return detail, nil
}

func autonomousIntentMatchesRoute(value PromptContext, action, role, head string) bool {
	if value.Route.TargetRole != role || value.Route.ExpectedHead != value.CurrentHead {
		return false
	}
	if head != "" && value.CurrentHead != head {
		return false
	}
	switch action {
	case "dispatch", "correct":
		return value.Route.Action == "dispatch"
	case "merge_candidate":
		return role == "lead" && value.Route.Action == "dispatch" && value.CurrentHead != ""
	case "plan_next_wave":
		return role == "lead" && value.Route.Action == "dispatch"
	default:
		return false
	}
}

func autonomousSummary(action string) string {
	switch action {
	case "merge_candidate":
		return "Deterministic Dashboard Autopilot Lead merge command composed from the typed merge Intent and current exact GitHub route."
	case "plan_next_wave":
		return "Deterministic Dashboard Autopilot next-Wave planning command composed after verified terminal Wave completion."
	default:
		return "Deterministic Dashboard Autopilot command composed from the current GitHub snapshot and exact Stage 3 route."
	}
}

func autonomousPrompt(value PromptContext, intentAction string, supplied ...AutonomousIntentDetail) string {
	detail := AutonomousIntentDetail{
		Action: intentAction, Repository: value.Repository.Owner + "/" + value.Repository.Repository,
		Role: value.Route.TargetRole, ExpectedHead: value.CurrentHead,
	}
	if len(supplied) == 1 {
		detail = supplied[0]
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Current objective\nExecute the current bounded CDDM action for Issue #%d — %s.\n\n", value.Issue.Number, value.Issue.Title)
	builder.WriteString("# Authoritative state\n")
	fmt.Fprintf(&builder, "Repository: %s\nLifecycle: %s\nRole: %s\nTyped Intent: %s\nRoute reason: %s\nExpected Head: %s\nContext hash: %s\n",
		detail.Repository, value.Issue.Lifecycle, value.Route.TargetRole,
		intentAction, value.Route.Reason, printableHead(value.CurrentHead), value.ContextHash)
	if detail.PRNumber > 0 {
		fmt.Fprintf(&builder, "Authorized PR: #%d\nExpected base branch: %s\n", detail.PRNumber, detail.ExpectedBaseRef)
	}
	if detail.WaveID != "" {
		fmt.Fprintf(&builder, "Completed Wave: %s\nProject Control Issue: #%d\n", detail.WaveID, detail.ControlIssue)
	}
	builder.WriteString("\n# Required next action\n")
	switch intentAction {
	case "merge_candidate":
		builder.WriteString("Act as the serialized Lead merge worker. Re-read the authoritative Issue, exact authorized PR, approved Candidate Head, fresh QA verdict and exact-Head CI. Merge only that PR into the stated base branch with expected-Head protection, then independently read back the merged PR and resulting commit before publishing `merged`.\n\n")
	case "plan_next_wave":
		builder.WriteString("Act as the persistent Lead planner after the stated previous Wave was independently verified terminal. Re-read the Project Control Issue and current repository backlog, then publish exactly one bounded `actions_ready` batch for the next coherent Wave.\n\n")
	default:
		builder.WriteString("Read the authoritative Issue contract, all current GitHub evidence and the latest applicable Lead authority. Perform the role operation required by the attached versioned role resource. Do not infer authority from chat history.\n\n")
	}
	builder.WriteString("# Scope and constraints\n")
	builder.WriteString("Stay inside the current Issue, typed Intent and Change boundary. Preserve the exact repository, role, Candidate identity and Head supplied by the Dashboard Command Header. Reconstruct durable state from GitHub before acting.\n\n")
	builder.WriteString("# Prohibited actions\n")
	if intentAction == "merge_candidate" {
		builder.WriteString("Do not merge a different PR, Head or base branch, do not retry an ambiguous merge, do not bypass required CI or QA, and do not claim success before GitHub read-back.\n\n")
	} else {
		builder.WriteString("Do not merge unless the typed Intent is `merge_candidate`. Do not redefine product scope, accept residual risk, disable required CI, launch another worker, replace the bound Candidate, or report evidence that was not read back.\n\n")
	}
	builder.WriteString("# Required evidence\n")
	builder.WriteString("Read back every consequential GitHub mutation, exact PR Head and required CI conclusion. Use the command_id from the Dashboard header in the terminal marker.\n\n")
	builder.WriteString("# Stop conditions\n")
	builder.WriteString("Stop with a bounded blocked result only for a real missing permission, unavailable dependency, required external decision or contradictory authoritative state. Ordinary implementation or review uncertainty is not a blocker.\n\n")
	builder.WriteString("# Initiative clause\n")
	builder.WriteString("Supporting work necessary for a coherent result remains in scope unless the Issue or role contract explicitly forbids it.\n\n")
	if value.Route.TargetRole == "qa" {
		builder.WriteString("# QA verdict contract\nReview only the exact current Head independently. Publish approved, changes_required or inconclusive with blocking findings and exact evidence. Do not repair the Candidate as QA.\n\n")
	}
	builder.WriteString("# Terminal worker_result\nPublish exactly one human-readable handoff and one live `cddm-worker-result/v2` marker correlated to the supplied command_id. Use only a terminal result allowed by the attached role resource. Do not start the next worker manually.\n")
	return builder.String()
}
