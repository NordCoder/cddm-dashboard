package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

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

func activeBlocker(results []ResultEvidence) (*ResultEvidence, []Warning) {
	unresolved := make([]ResultEvidence, 0)
	warnings := make([]Warning, 0)
	for _, result := range results {
		if result.Role == "lead" && result.Effective && len(unresolved) > 0 && leadResolutionIntent(result) {
			active := unresolved[0]
			if !leadResolutionActionable(result) {
				warnings = append(warnings, warning(result.CommentID, "non_actionable_blocker_resolution", "Lead result includes blocker-resolution fields but does not define a safe Implementor/QA resume or owner escalation transition"))
			} else {
				matches, understood := resolvesComment(result.Resolves, active.CommentID)
				switch {
				case matches:
					unresolved = unresolvedAfterResolution(unresolved, result.Resolves)
				case len(result.Resolves) == 0:
					warnings = append(warnings, warning(result.CommentID, "missing_blocker_resolution", fmt.Sprintf("Lead result does not identify active blocker comment %d in resolves", active.CommentID)))
				case !understood:
					warnings = append(warnings, warning(result.CommentID, "unknown_blocker_resolution", "Lead resolves value is preserved but cannot be correlated to the active blocker"))
				default:
					warnings = append(warnings, warning(result.CommentID, "unmatched_blocker_resolution", fmt.Sprintf("Lead resolves does not reference active blocker comment %d", active.CommentID)))
				}
			}
		}
		if result.Status == "blocked" && result.Effective && !containsBlocker(unresolved, result.CommentID) {
			unresolved = append(unresolved, result)
			if len(unresolved) > 1 {
				warnings = append(warnings, warning(result.CommentID, "additional_unresolved_blocker", fmt.Sprintf("blocked result is queued behind unresolved blocker comment %d", unresolved[0].CommentID)))
			}
		}
	}
	if len(unresolved) == 0 {
		return nil, warnings
	}
	active := unresolved[0]
	return &active, warnings
}

func unresolvedAfterResolution(blockers []ResultEvidence, resolves json.RawMessage) []ResultEvidence {
	remaining := make([]ResultEvidence, 0, len(blockers))
	for _, blocker := range blockers {
		matches, _ := resolvesComment(resolves, blocker.CommentID)
		if !matches {
			remaining = append(remaining, blocker)
		}
	}
	return remaining
}

func containsBlocker(blockers []ResultEvidence, commentID int64) bool {
	for _, blocker := range blockers {
		if blocker.CommentID == commentID {
			return true
		}
	}
	return false
}

func leadResolutionIntent(result ResultEvidence) bool {
	return result.Decision != "" || result.ResumeRole != "" || len(result.Resolves) > 0 || result.EscalateTo != ""
}

func leadResolutionActionable(result ResultEvidence) bool {
	if result.Role != "lead" || !result.Effective {
		return false
	}
	if result.EscalateTo == "owner" || result.Decision == "owner_required" {
		return true
	}
	return (result.Status == "completed" || result.Status == "no_op") &&
		leadResumeRoleAllowed(result.ResumeRole) && oneOf(result.Decision, "continue", "correct", "resume")
}

func leadResumeRoleAllowed(role string) bool {
	return oneOf(role, "implementor", "qa")
}

func resolvesComment(raw json.RawMessage, commentID int64) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return false, false
	}
	return resolutionValueMatches(value, commentID)
}

func resolutionValueMatches(value any, commentID int64) (bool, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		return err == nil && parsed == commentID, err == nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(typed, "#")), 10, 64)
		return err == nil && parsed == commentID, err == nil
	case []any:
		understood := false
		for _, item := range typed {
			matches, itemUnderstood := resolutionValueMatches(item, commentID)
			understood = understood || itemUnderstood
			if matches {
				return true, true
			}
		}
		return false, understood
	case map[string]any:
		for _, key := range []string{"comment_id", "id"} {
			if item, ok := typed[key]; ok {
				return resolutionValueMatches(item, commentID)
			}
		}
	}
	return false, false
}
