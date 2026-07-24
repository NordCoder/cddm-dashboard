package planning

import (
	"fmt"
	"strings"
)

func RenderFallback(context PromptContext) PromptPlan {
	plan := PromptPlan{
		Version: PromptPlanVersion,
		Action:  context.Route.Action, TargetRole: context.Route.TargetRole, LaneKey: context.Route.LaneKey,
		Summary:       "Deterministic worker prompt composed from the persisted GitHub snapshot and Stage 3 route.",
		Reason:        context.Route.Reason,
		Risk:          fallbackRisk(context),
		RequiresOwner: context.Route.Action == "owner_attention",
		ExpectedHead:  context.CurrentHead, ExpectedEvent: context.ExpectedEvent,
		Guards:            append([]string(nil), context.Route.Guards...),
		ProhibitedActions: append([]string(nil), mandatoryProhibitedActions...),
		Confidence:        1,
		Source: SourceMetadata{
			Kind: SourceTemplateFallback, Runtime: "static_template", Mode: ModeFallback,
			ContextHash: context.ContextHash,
		},
	}
	if context.Route.Action == "dispatch" || (context.Route.Action == "manual_attention" && context.Route.TargetRole != "") {
		plan.Prompt = fallbackPrompt(context)
	}
	return plan
}

func fallbackRisk(context PromptContext) string {
	if context.Route.Action == "owner_attention" {
		return "Owner decision is required; no worker prompt is emitted."
	}
	if context.Route.Action == "none" {
		return "No safe action is currently routable."
	}
	if context.ActiveBlocker != nil {
		return "An active blocker must be resolved according to the Lead-first route."
	}
	return "Static wording is conservative; Stage 3 route and exact-Head guards remain authoritative."
}

func fallbackPrompt(context PromptContext) string {
	var builder strings.Builder
	builder.WriteString("# Current objective\n")
	builder.WriteString(fmt.Sprintf("Issue #%d — %s\n\n", context.Issue.Number, context.Issue.Title))
	if strings.TrimSpace(context.Issue.Body) != "" {
		builder.WriteString(context.Issue.Body)
		builder.WriteString("\n\n")
	} else {
		builder.WriteString("The persisted Issue body is empty. Use the Issue title and authoritative evidence below without inventing requirements.\n\n")
	}
	builder.WriteString("# Authoritative state\n")
	builder.WriteString(fmt.Sprintf("Repository: %s/%s\nLifecycle: %s\nAttention: %s (%s)\nCurrent Candidate Head: %s\nCI: %s/%s\nRoute reason: %s\nContext hash: %s\n\n",
		context.Repository.Owner, context.Repository.Repository, context.Issue.Lifecycle,
		context.Issue.Attention.Kind, context.Issue.Attention.Code, printableHead(context.CurrentHead),
		blankAs(context.CI.Status, "unknown"), blankAs(context.CI.Conclusion, "unknown"), context.Route.Reason, context.ContextHash))
	builder.WriteString("# Required next action\n")
	builder.WriteString(fmt.Sprintf("Act as %s on lane %s. Preserve action=%s and expected Head=%s.\n\n", context.Route.TargetRole, context.Route.LaneKey, context.Route.Action, printableHead(context.CurrentHead)))
	builder.WriteString("# Scope and constraints\n")
	builder.WriteString("Use the authoritative Issue contract embedded above and the repository evidence available to the worker. Keep the implementation or review coherent and inside the approved scope. Stage 3 routing authority cannot be changed by the worker.\n\n")
	builder.WriteString("# Prohibited actions\n")
	builder.WriteString("Do not merge the pull request.\nDo not write to GitHub.\nDo not dispatch through a browser.\nDo not approve a scope change.\nDo not accept residual risk.\nDo not disable required CI.\nDo not claim checks, commits, handoffs, verdicts, or evidence that do not exist.\n\n")
	builder.WriteString("# Required evidence\n")
	builder.WriteString("Read all Issue comments and the latest Lead Dispatch through the authorized GitHub transport. Correlate Candidate identity and exact Head before acting. Record exact-Head CI evidence when required.\n\n")
	builder.WriteString("# Stop conditions\n")
	builder.WriteString("Stop with blocked only when continuation requires a real external decision, missing mandatory information, unavailable permissions, or infrastructure. Do not block on ordinary engineering uncertainty that can be resolved by repository reads, tests, standard design choices, or a focused experiment.\n\n")
	builder.WriteString("# Initiative Clause\n")
	builder.WriteString("The absence of a named file, function, or technical step is not a prohibition. Supporting work necessary for a safe coherent outcome remains in scope unless the authoritative contract forbids it.\n\n")
	if context.Route.TargetRole == "qa" {
		builder.WriteString("# QA Verdict Contract\n")
		builder.WriteString("Review the exact current Head independently. Publish one verdict: approved, changes_required, or inconclusive, with exact evidence and the reviewed Head. Do not repair the Candidate as QA.\n\n")
	}
	builder.WriteString("# Terminal worker_result\n")
	builder.WriteString("Before ending, publish one human-readable Issue result and exactly one live `<!-- supervisor:event ... -->` envelope with event=`worker_result`, the routed role, and status=`completed`, `no_op`, or `blocked`. Candidate-bound completed/no_op results must include the exact full Head. QA must also include the verdict. Even no-op work requires the terminal result.\n")
	return builder.String()
}

func printableHead(value string) string { return blankAs(value, "none") }

func blankAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
