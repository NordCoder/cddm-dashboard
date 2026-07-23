package workflow

import (
	"fmt"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"sort"
	"strings"
	"time"
)

func effectiveCI(ci supervisor.CISummary, currentHead string, warnings []Warning) (supervisor.CISummary, []Warning) {
	if ci.HeadSHA == "" && ci.Status == "" && ci.Conclusion == "" && ci.Source == "" && ci.DetailsURL == "" && ci.UpdatedAt.IsZero() {
		return supervisor.CISummary{}, warnings
	}
	if currentHead == "" {
		return supervisor.CISummary{}, append(warnings, Warning{Code: "unbound_ci_summary", Message: "CI summary exists but no current Candidate Head is selected"})
	}
	if ci.HeadSHA == "" {
		return supervisor.CISummary{}, append(warnings, Warning{Code: "unbound_ci_summary", Message: "CI summary is missing the exact Head SHA and cannot affect routing"})
	}
	if ci.HeadSHA != currentHead {
		return supervisor.CISummary{}, append(warnings, Warning{Code: "stale_ci_summary", Message: fmt.Sprintf("CI summary Head %s does not match current Head %s", ci.HeadSHA, currentHead)})
	}
	return ci, warnings
}

func ciFailed(ci supervisor.CISummary) bool {
	conclusion := normalizeToken(ci.Conclusion)
	return oneOf(conclusion, "failure", "failed", "cancelled", "timed_out", "action_required", "startup_failure")
}

func ciSucceeded(ci supervisor.CISummary) bool {
	return normalizeToken(ci.Conclusion) == "success"
}

func latestTerminalEvidenceUnsafe(state WorkUnitState) bool {
	for index := len(state.ParsedComments) - 1; index >= 0; index-- {
		parsed := state.ParsedComments[index]
		if parsed.Level != ParseLevelEnvelope && parsed.Level != ParseLevelHeading {
			continue
		}
		return parsed.HardError != nil || parsed.Event == nil || !parsed.TransitionSafe
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
	if state.CurrentHead == "" {
		return false
	}
	if state.QAApprovedHead == state.CurrentHead {
		return false
	}
	if latest := state.LatestResults.QA; latest != nil && latest.Effective && !latest.Stale && latest.Head == state.CurrentHead {
		return false
	}
	for _, parsed := range state.ParsedComments {
		if parsed.Event == nil || parsed.Event.Role != "qa" || parsed.Event.Verdict != "approved" {
			continue
		}
		if parsed.Event.Head != "" && parsed.Event.Head != state.CurrentHead {
			return true
		}
	}
	return false
}

func hasProtocolProblem(state WorkUnitState) bool {
	latestCommentID := int64(0)
	for index := len(state.ParsedComments) - 1; index >= 0; index-- {
		parsed := state.ParsedComments[index]
		if parsed.Level != ParseLevelEnvelope && parsed.Level != ParseLevelHeading {
			continue
		}
		latestCommentID = parsed.CommentID
		if parsed.HardError != nil || parsed.Level == ParseLevelHeading || parsed.Event == nil || !parsed.TransitionSafe {
			return true
		}
		break
	}
	for _, warning := range state.Warnings {
		if warning.CommentID != 0 && warning.CommentID != latestCommentID {
			continue
		}
		if oneOf(warning.Code, "unknown_version", "unknown_event", "unknown_role", "unknown_status", "unknown_verdict", "unresolved_head_prefix", "missing_candidate_head", "missing_current_head", "missing_dispatch_correlation", "role_mismatch", "stale_ci_summary", "unbound_ci_summary", "missing_blocker_resolution", "unmatched_blocker_resolution", "unknown_blocker_resolution", "non_actionable_blocker_resolution", "additional_unresolved_blocker", "invalid_lead_resume_role") {
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
