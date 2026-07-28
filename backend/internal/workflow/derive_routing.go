package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
	if latestTerminalEvidenceUnsafe(state) {
		return manualLeadRoute(project, state, "unsafe_terminal_result", "latest terminal evidence is malformed or incomplete and requires Lead/manual review")
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
	dashboardBound := resultExtensionString(*latest, "command_id") != ""

	if leadOwnerEscalation(*latest) {
		if state.ActiveBlocker != nil && state.ActiveBlocker.CommentID != latest.CommentID {
			matches, _ := resolvesComment(latest.Resolves, state.ActiveBlocker.CommentID)
			if !matches {
				return manualLeadRoute(project, state, "unresolved_active_blocker", "latest Lead escalation did not correlate to the active blocker")
			}
		}
		return Route{Action: "owner_attention", ReasonCode: "owner_required", Reason: "Lead requires an Owner decision; the correlated blocker remains pending until Owner acts", ExpectedHead: state.CurrentHead, Guards: []string{"lead_first_blocker_flow", "active_blocker_pending", "no_worker_dispatch"}, Warnings: state.Warnings}
	}

	if dashboardBound && latest.Role == "qa" && latest.Status == "inconclusive" {
		if resultExtensionString(*latest, "reason_code") == "exact_candidate_ci_queued" && resultExtensionInt(*latest, "blocking_findings") == 0 {
			if ciSucceeded(state.CI) {
				next := dispatchRoute(project, state, "qa", "qa_retry_after_ci", "exact-Candidate CI succeeded; request a fresh QA session for the same Head")
				next.Guards = append(next.Guards, "ci_success", "fresh_qa_session")
				return next
			}
			route.ReasonCode = "waiting_for_ci"
			route.Reason = "QA is inconclusive only because exact-Candidate CI is queued or missing; preserve the current Candidate"
			route.Guards = append(route.Guards, "no_correction_cycle", "same_candidate")
			return route
		}
		return dispatchRoute(project, state, "lead", "qa_inconclusive", "QA could not reach a conclusive verdict and requires Lead review")
	}

	if state.ActiveBlocker != nil && latest.CommentID != state.ActiveBlocker.CommentID {
		return manualLeadRoute(project, state, "unresolved_active_blocker", "latest Lead result did not correlate to and safely resolve the active blocker")
	}
	if latest.Status == "blocked" {
		if latest.Role == "implementor" || latest.Role == "qa" {
			return dispatchRoute(project, state, "lead", "lead_first_blocker", "blocked Implementor or QA result must be resolved by Lead first")
		}
		return Route{
			Action: "manual_attention", ReasonCode: "lead_blocked_requires_decision",
			Reason:       "Lead blocked result requires an explicit follow-up decision or owner_required escalation; automatic redispatch would repeat the same Lead lane",
			ExpectedHead: state.CurrentHead, Guards: append(guardsForHead(state.CurrentHead), "manual_confirmation", "no_repeat_dispatch"), Warnings: state.Warnings,
		}
	}

	if latest.Role == "implementor" {
		if dashboardBound {
			switch latest.Status {
			case "continue":
				return dispatchRoute(project, state, "implementor", "implementation_continue", "Implementor requested another bounded turn in the same Change")
			case "no_op":
				return dispatchRoute(project, state, "lead", "implementation_no_op", "Implementor reported no source change; Lead must verify the Change outcome")
			}
		}
		if ciFailed(state.CI) {
			return dispatchRoute(project, state, "implementor", "candidate_ci_failed", "current Candidate CI failed and requires Implementor correction")
		}
		if !ciSucceeded(state.CI) {
			route.ReasonCode = "waiting_for_ci"
			route.Reason = "current Candidate has no exact-Head successful CI conclusion yet"
			return route
		}
	}

	switch latest.Role {
	case "implementor":
		if latest.Status == "completed" || (!dashboardBound && latest.Status == "no_op") {
			if qaRequired(workflowMode) {
				next := dispatchRoute(project, state, "qa", "implementation_completed", "Implementor terminal result advances to independent QA")
				next.Guards = append(next.Guards, "ci_success")
				if dashboardBound {
					next.Guards = append(next.Guards, "fresh_qa_session")
				}
				return next
			}
			next := dispatchRoute(project, state, "lead", "implementation_completed_no_qa", "Implementor terminal result advances to Lead because QA is not required")
			next.Guards = append(next.Guards, "ci_success")
			return next
		}
	case "qa":
		switch latest.Verdict {
		case "approved":
			if dashboardBound && !ciSucceeded(state.CI) {
				route.ReasonCode = "waiting_for_ci"
				route.Reason = "QA approval is preserved but cannot advance until exact-Candidate CI succeeds"
				return route
			}
			return dispatchRoute(project, state, "lead", "qa_approved", "QA approved the current exact Head")
		case "changes_required":
			if dashboardBound {
				return dispatchRoute(project, state, "lead", "qa_changes_required", "QA requested Candidate-bound changes; Lead must bound the correction before Implementor resume")
			}
			return dispatchRoute(project, state, "implementor", "qa_changes_required", "QA requested changes on the current exact Head")
		case "inconclusive":
			return dispatchRoute(project, state, "lead", "qa_inconclusive", "QA could not reach a conclusive verdict")
		}
	case "lead":
		if dashboardBound {
			switch latest.Decision {
			case "dispatch":
				if !oneOf(latest.ResumeRole, "lead", "implementor", "qa") {
					return manualLeadRoute(project, state, "invalid_lead_resume_role", "Lead dispatch result names an unsupported role")
				}
				return dispatchRoute(project, state, latest.ResumeRole, "lead_dispatch", "Lead selected the next validated worker role")
			case "continue":
				return dispatchRoute(project, state, "lead", "lead_continue", "Lead requested another bounded Lead turn")
			case "correct":
				return dispatchRoute(project, state, "implementor", "lead_correction", "Lead bounded a correction for the existing Change")
			case "ready_to_merge":
				return mergeGateRoute(state)
			}
		} else if latest.ResumeRole != "" && oneOf(latest.Decision, "continue", "correct", "resume") {
			if !leadResumeRoleAllowed(latest.ResumeRole) {
				return manualLeadRoute(project, state, "invalid_lead_resume_role", "Lead automatic continuation may resume only Implementor or QA; resuming Lead would repeat the same lane")
			}
			return dispatchRoute(project, state, latest.ResumeRole, "lead_resume", "Lead decision resumes the validated worker role")
		}
	}
	return manualLeadRoute(project, state, "unhandled_terminal_result", "terminal result is preserved but has no safe automatic route")
}

func mergeGateRoute(state WorkUnitState) Route {
	guards := append(guardsForHead(state.CurrentHead), "ci_success", "qa_approved_current_head", "candidate_mergeable", "no_active_blocker", "manual_merge_authority")
	if state.Candidate.Current == nil || state.CurrentHead == "" {
		return Route{Action: "manual_attention", TargetRole: "lead", ReasonCode: "merge_gate_missing_candidate", Reason: "Lead requested merge review without one current Candidate", ExpectedHead: state.CurrentHead, Guards: guards, Warnings: state.Warnings}
	}
	if !ciSucceeded(state.CI) || state.QAApprovedHead != state.CurrentHead || state.ActiveBlocker != nil {
		return Route{Action: "manual_attention", TargetRole: "lead", ReasonCode: "merge_gate_incomplete", Reason: "Lead requested merge review but exact-Candidate CI, fresh QA or blocker gates are incomplete", ExpectedHead: state.CurrentHead, Guards: guards, Warnings: state.Warnings}
	}
	mergeable := normalizeToken(state.Candidate.Current.MergeableState)
	if mergeable != "clean" && mergeable != "mergeable" && mergeable != "unstable" {
		return Route{Action: "manual_attention", TargetRole: "lead", ReasonCode: "merge_gate_not_mergeable", Reason: "current primary PR is not known mergeable", ExpectedHead: state.CurrentHead, Guards: guards, Warnings: state.Warnings}
	}
	return Route{Action: "merge_gate", TargetRole: "lead", ReasonCode: "ready_to_merge_verification", Reason: "all observable merge gates are satisfied; merge still requires explicit Lead authority and expected-Head protection", ExpectedHead: state.CurrentHead, Guards: guards, Warnings: state.Warnings}
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

func resultExtensionString(result ResultEvidence, key string) string {
	var value string
	if raw := result.Extensions[key]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

func resultExtensionInt(result ResultEvidence, key string) int {
	var value int
	if raw := result.Extensions[key]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}
