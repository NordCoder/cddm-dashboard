package workflow

import (
	"fmt"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"sort"
	"strings"
)

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
