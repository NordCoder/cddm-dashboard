package planning

import (
	"context"
	"fmt"
	"strings"
)

// GenerateAutonomous persists an internally rendered Prompt Plan for the exact
// current deterministic route. Caller input can constrain role and Head, but
// cannot supply prompt text or replace Stage 3 routing authority.
func (s *Service) GenerateAutonomous(ctx context.Context, projectID int64, issueNumber int, expectedRole, expectedHead string) (GenerationResult, error) {
	contextValue, contextJSON, err := s.freshContext(ctx, projectID, issueNumber)
	if err != nil {
		return GenerationResult{}, err
	}
	if contextValue.Route.Action != "dispatch" || contextValue.Route.TargetRole != strings.TrimSpace(expectedRole) || contextValue.CurrentHead != strings.TrimSpace(expectedHead) || contextValue.Route.ExpectedHead != contextValue.CurrentHead {
		return GenerationResult{}, fmt.Errorf("autonomous route no longer matches the claimed Intent")
	}

	createdAt := s.now().UTC()
	plan := RenderFallback(contextValue)
	plan.Summary = "Deterministic Dashboard Autopilot command composed from the current GitHub snapshot and exact Stage 3 route."
	plan.Reason = contextValue.Route.Reason
	plan.Risk = "Autonomous materialization remains bounded by the exact current route, Candidate Head, provisioned role session and command-bound result protocol."
	plan.Prompt = autonomousPrompt(contextValue)
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

func autonomousPrompt(value PromptContext) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Current objective\nExecute the current bounded CDDM action for Issue #%d — %s.\n\n", value.Issue.Number, value.Issue.Title)
	builder.WriteString("# Authoritative state\n")
	fmt.Fprintf(&builder, "Repository: %s/%s\nLifecycle: %s\nRole: %s\nRoute reason: %s\nExpected Head: %s\nContext hash: %s\n\n",
		value.Repository.Owner, value.Repository.Repository, value.Issue.Lifecycle, value.Route.TargetRole,
		value.Route.Reason, printableHead(value.CurrentHead), value.ContextHash)
	builder.WriteString("# Required next action\n")
	builder.WriteString("Read the authoritative Issue contract, all current GitHub evidence and the latest applicable Lead authority. Perform the role operation required by the attached versioned role resource. Do not infer authority from chat history.\n\n")
	builder.WriteString("# Scope and constraints\n")
	builder.WriteString("Stay inside the current Issue and Change boundary. Preserve the exact repository, role, Candidate identity and Head supplied by the Dashboard Command Header. Reconstruct durable state from GitHub before acting.\n\n")
	builder.WriteString("# Prohibited actions\n")
	builder.WriteString("Do not merge unless the command is an explicit Lead merge command. Do not redefine product scope, accept residual risk, disable required CI, launch another worker, replace the bound Candidate, or report evidence that was not read back.\n\n")
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
