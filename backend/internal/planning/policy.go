package planning

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

var mandatoryProhibitedActions = []string{
	"accept_residual_risk",
	"approve_scope_change",
	"browser_dispatch",
	"disable_required_ci",
	"github_write",
	"merge",
}

var requiredPromptSections = []string{
	"current objective",
	"authoritative state",
	"required next action",
	"scope and constraints",
	"prohibited actions",
	"required evidence",
	"stop conditions",
	"initiative clause",
	"terminal worker_result",
}

var forbiddenAuthorityPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bmerge\s+(?:the\s+)?(?:pull request|pr|candidate)\b`),
	regexp.MustCompile(`(?i)\b(?:write|push|commit|comment|label|close|approve)\s+(?:to|on|the)\s+github\b`),
	regexp.MustCompile(`(?i)\bdispatch\s+(?:(?:through|to)\s+)?(?:a\s+)?browser\b`),
	regexp.MustCompile(`(?i)\bapprove\s+(?:a\s+)?scope\s+change\b`),
	regexp.MustCompile(`(?i)\baccept\s+(?:the\s+)?residual\s+risk\b`),
	regexp.MustCompile(`(?i)\bdisable\s+(?:the\s+)?required\s+ci\b`),
}

func ValidatePlan(context PromptContext, plan PromptPlan, current PromptContext, now time.Time) PolicyDecision {
	violations := make([]Violation, 0)
	add := func(code, field, message string) {
		violations = append(violations, Violation{Code: code, Field: field, Message: message})
	}

	if context.ContextHash == "" || current.ContextHash == "" || context.ContextHash != current.ContextHash {
		add("stale_context", "source.context_hash", "authoritative PromptContext changed after plan context creation")
	}
	if context.CurrentHead != current.CurrentHead || !sameRoute(context.Route, current.Route) {
		add("stale_route", "expected_head", "current Head or deterministic Stage 3 route changed")
	}
	if hasWarningCode(context.Warnings, issueContractIncompleteWarning) {
		add("incomplete_issue_contract", "issue.body", "authoritative Issue contract exceeded its bounded PromptContext budget")
	}
	if plan.Version != PromptPlanVersion {
		add("wrong_version", "v", fmt.Sprintf("PromptPlan version must be %d", PromptPlanVersion))
	}
	if plan.Action != context.Route.Action {
		add("wrong_action", "action", fmt.Sprintf("action must match Stage 3 route %q", context.Route.Action))
	}
	if plan.TargetRole != context.Route.TargetRole {
		add("wrong_role", "target_role", fmt.Sprintf("target_role must match Stage 3 route %q", context.Route.TargetRole))
	}
	if plan.LaneKey != context.Route.LaneKey {
		add("wrong_lane", "lane_key", "lane_key must match deterministic Stage 3 route")
	}
	if plan.ExpectedHead != context.CurrentHead || plan.ExpectedHead != context.Route.ExpectedHead {
		add("wrong_head", "expected_head", "expected_head must equal the exact current Candidate Head")
	}
	if plan.ExpectedEvent != context.ExpectedEvent {
		add("wrong_expected_event", "expected_event", "expected_event must match the route contract")
	}
	if plan.Source.ContextHash != context.ContextHash {
		add("wrong_context_hash", "source.context_hash", "source context_hash must reference the exact PromptContext")
	}
	if plan.Source.Kind != SourceOpenCode && plan.Source.Kind != SourceTemplateFallback {
		add("unsupported_source", "source.kind", "source kind must be opencode or template_fallback")
	}
	if plan.Source.Kind == SourceOpenCode && (plan.Source.Runtime != "opencode" || plan.Source.Mode != ModeOpenCode) {
		add("wrong_runtime", "source", "OpenCode plan must identify runtime and mode as opencode")
	}
	if plan.Source.Kind == SourceTemplateFallback && plan.Source.Mode != ModeFallback {
		add("wrong_fallback_mode", "source.mode", "template fallback must identify fallback mode")
	}
	if plan.Confidence < 0 || plan.Confidence > 1 {
		add("invalid_confidence", "confidence", "confidence must be between 0 and 1")
	}
	if strings.TrimSpace(plan.Summary) == "" || strings.TrimSpace(plan.Reason) == "" || strings.TrimSpace(plan.Risk) == "" {
		add("missing_explanation", "summary", "summary, reason and risk must be non-empty")
	}

	requiresOwner := context.Route.Action == "owner_attention" || context.Issue.Attention.Kind == workflow.AttentionOwnerRequired
	if plan.RequiresOwner != requiresOwner {
		add("wrong_owner_semantics", "requires_owner", "requires_owner must match current Owner-attention semantics")
	}
	if context.ActiveBlocker != nil && context.Route.Action == "dispatch" && context.Route.TargetRole != "lead" {
		add("active_blocker_bypass", "target_role", "active blocker may only route to Lead before another worker")
	}
	if context.Candidate.Ambiguous && plan.Action == "dispatch" {
		add("ambiguous_candidate_dispatch", "action", "ambiguous Candidate cannot produce worker dispatch")
	}

	if !sameStrings(plan.Guards, context.Route.Guards) {
		add("wrong_guards", "guards", "guards must exactly preserve deterministic Stage 3 route guards")
	}
	for _, required := range mandatoryProhibitedActions {
		if !contains(plan.ProhibitedActions, required) {
			add("missing_prohibition", "prohibited_actions", fmt.Sprintf("required prohibition %q is missing", required))
		}
	}
	if contains(plan.ProhibitedActions, "") {
		add("invalid_prohibition", "prohibited_actions", "prohibited action names must be non-empty")
	}

	workerPromptAllowed := plan.Action == "dispatch" || (plan.Action == "manual_attention" && plan.TargetRole != "")
	if plan.Action == "owner_attention" || plan.Action == "none" || (plan.Action == "manual_attention" && plan.TargetRole == "") {
		workerPromptAllowed = false
	}
	if !workerPromptAllowed {
		if strings.TrimSpace(plan.Prompt) != "" {
			add("worker_prompt_not_allowed", "prompt", "current route must not create a worker-chat prompt")
		}
	} else {
		lowerPrompt := strings.ToLower(plan.Prompt)
		for _, section := range requiredPromptSections {
			if !strings.Contains(lowerPrompt, section) {
				add("missing_prompt_section", "prompt", fmt.Sprintf("worker prompt is missing section %q", section))
			}
		}
		if !strings.Contains(lowerPrompt, "completed") || !strings.Contains(lowerPrompt, "no_op") || !strings.Contains(lowerPrompt, "blocked") {
			add("missing_terminal_statuses", "prompt", "worker prompt must require completed, no_op or blocked terminal result")
		}
		if !strings.Contains(lowerPrompt, "supervisor:event") || !strings.Contains(lowerPrompt, "worker_result") {
			add("missing_terminal_contract", "prompt", "worker prompt must require the supervisor worker_result envelope")
		}
		if plan.TargetRole == "qa" && !strings.Contains(lowerPrompt, "qa verdict contract") {
			add("missing_qa_contract", "prompt", "QA route must include a QA Verdict Contract")
		}
		if grantsForbiddenAuthority(plan.Prompt) {
			add("forbidden_authority", "prompt", "prompt grants prohibited merge, GitHub-write, browser, scope, risk or CI authority")
		}
		if (strings.Contains(lowerPrompt, "ci passed") || strings.Contains(lowerPrompt, "ci succeeded")) && strings.ToLower(context.CI.Conclusion) != "success" {
			add("unsupported_claim", "prompt", "prompt claims successful CI that is absent from the exact-Head context")
		}
		if strings.Contains(lowerPrompt, "qa approved") && (context.LatestResults.QA == nil || context.LatestResults.QA.Verdict != "approved" || !context.LatestResults.QA.Effective) {
			add("unsupported_claim", "prompt", "prompt claims QA approval that is absent from authoritative evidence")
		}
	}

	planBytes, _ := CanonicalPlanBytes(plan)
	if redactText(string(planBytes)) != string(planBytes) {
		add("secret_detected", "plan", "PromptPlan contains credential-like material")
	}
	status := StatusApproved
	for _, violation := range violations {
		if strings.HasPrefix(violation.Code, "stale_") {
			status = StatusStale
			break
		}
		status = StatusRejected
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Code != violations[j].Code {
			return violations[i].Code < violations[j].Code
		}
		if violations[i].Field != violations[j].Field {
			return violations[i].Field < violations[j].Field
		}
		return violations[i].Message < violations[j].Message
	})
	return PolicyDecision{
		Status: status, Violations: violations, ContextHash: context.ContextHash,
		PlanHash: hashBytes(planBytes), DecidedAt: now.UTC(),
	}
}

func sameRoute(left, right workflow.Route) bool {
	return left.Action == right.Action && left.TargetRole == right.TargetRole && left.LaneKey == right.LaneKey &&
		left.ExpectedHead == right.ExpectedHead && left.ReasonCode == right.ReasonCode && sameStrings(left.Guards, right.Guards)
}

func sameStrings(left, right []string) bool {
	left = sortedUnique(left)
	right = sortedUnique(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func hasWarningCode(warnings []workflow.Warning, code string) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func grantsForbiddenAuthority(prompt string) bool {
	for _, line := range strings.Split(prompt, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if lower == "" {
			continue
		}
		for _, pattern := range forbiddenAuthorityPatterns {
			for _, match := range pattern.FindAllStringIndex(lower, -1) {
				if !matchIsExplicitProhibition(lower, match[0], match[1]) {
					return true
				}
			}
		}
	}
	return false
}

func matchIsExplicitProhibition(line string, start, end int) bool {
	prefix := strings.TrimSpace(line[:start])
	prefix = strings.TrimRight(prefix, " \t:;,-")
	for _, phrase := range []string{
		"do not",
		"must not",
		"never",
		"may not",
		"cannot",
		"can not",
		"can't",
		"forbidden to",
		"prohibited from",
	} {
		if strings.HasSuffix(prefix, phrase) {
			return true
		}
	}

	suffix := strings.TrimSpace(line[end:])
	for _, phrase := range []string{
		"is prohibited",
		"is forbidden",
		"is not allowed",
		"is disallowed",
	} {
		if strings.HasPrefix(suffix, phrase) {
			return true
		}
	}
	return false
}
