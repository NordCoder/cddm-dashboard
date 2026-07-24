package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

const (
	defaultEvidenceLimit     = 12
	defaultEvidenceChars     = 4000
	minimumSafeEvidenceLimit = 8
)

type ContextOptions struct {
	EvidenceLimit int
	EvidenceChars int
}

var (
	authorizationPattern   = regexp.MustCompile(`(?i)(authorization\s*:\s*)([^\r\n]+)`)
	tokenPattern           = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]{16,}|sk-[A-Za-z0-9_-]{16,})\b`)
	assignmentPattern      = regexp.MustCompile(`(?i)\b([A-Z][A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|API_KEY))\s*[:=]\s*([^\s,;]+)`)
	credentialValuePattern = regexp.MustCompile(`(?i)(["']?(?:authorization|credential|password|secret|token|api[_-]?key)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;}\]]+)`)
)

func BuildContext(snapshot supervisor.ProjectSnapshot, state workflow.WorkUnitState, options ContextOptions) (PromptContext, []byte, error) {
	limit := options.EvidenceLimit
	if limit == 0 {
		limit = defaultEvidenceLimit
	}
	if limit < minimumSafeEvidenceLimit {
		return PromptContext{}, nil, fmt.Errorf("evidence limit must be at least %d", minimumSafeEvidenceLimit)
	}
	chars := options.EvidenceChars
	if chars == 0 {
		chars = defaultEvidenceChars
	}
	if chars < 256 {
		return PromptContext{}, nil, fmt.Errorf("evidence character limit must be at least 256")
	}

	issue, _ := findSnapshotIssue(snapshot.Issues, state.Identity.IssueGitHubID, state.Identity.IssueNumber)

	context := PromptContext{
		Version: PromptContextVersion,
		Repository: RepositoryIdentity{
			ProjectID:    snapshot.Project.ID,
			Owner:        redactText(snapshot.Project.Owner),
			Repository:   redactText(snapshot.Project.Repository),
			WorkflowMode: redactText(snapshot.Project.WorkflowMode),
		},
		Issue: IssueIdentity{
			GitHubID:  state.Identity.IssueGitHubID,
			Number:    state.Identity.IssueNumber,
			Title:     redactText(state.Identity.Title),
			Body:      truncateUTF8(redactText(issue.Body), chars),
			URL:       redactText(state.Identity.URL),
			Lifecycle: state.Lifecycle,
			Attention: redactAttention(state.Attention),
		},
		Candidate:     sanitizeCandidateState(state.Candidate),
		CurrentHead:   state.CurrentHead,
		CI:            sanitizeCI(state.CI),
		LatestResults: latestSummary(state.LatestResults),
		ActiveBlocker: resultSummary(state.ActiveBlocker),
		Route:         sanitizeRoute(state.Route),
		ExpectedEvent: expectedEvent(state.Route),
		Warnings:      sanitizeWarnings(state.Warnings),
		Evidence:      boundedEvidence(state, limit, chars),
	}

	canonical, err := canonicalContextBytes(context)
	if err != nil {
		return PromptContext{}, nil, err
	}
	context.ContextHash = hashBytes(canonical)
	canonical, err = json.Marshal(context)
	if err != nil {
		return PromptContext{}, nil, fmt.Errorf("serialize prompt context: %w", err)
	}
	return context, canonical, nil
}

func findSnapshotIssue(issues []supervisor.Issue, githubID int64, issueNumber int) (supervisor.Issue, bool) {
	for _, issue := range issues {
		if issue.GitHubID == githubID && issue.Number == issueNumber {
			return issue, true
		}
	}
	return supervisor.Issue{}, false
}

func canonicalContextBytes(context PromptContext) ([]byte, error) {
	copy := context
	copy.ContextHash = ""
	copy.Warnings = append([]workflow.Warning(nil), copy.Warnings...)
	sort.Slice(copy.Warnings, func(i, j int) bool {
		if copy.Warnings[i].CommentID != copy.Warnings[j].CommentID {
			return copy.Warnings[i].CommentID < copy.Warnings[j].CommentID
		}
		if copy.Warnings[i].Code != copy.Warnings[j].Code {
			return copy.Warnings[i].Code < copy.Warnings[j].Code
		}
		return copy.Warnings[i].Message < copy.Warnings[j].Message
	})
	copy.Route.Guards = sortedUnique(copy.Route.Guards)
	copy.Route.Warnings = sanitizeWarnings(copy.Route.Warnings)
	copy.Evidence = append([]Evidence(nil), copy.Evidence...)
	sort.Slice(copy.Evidence, func(i, j int) bool {
		if !copy.Evidence[i].CreatedAt.Equal(copy.Evidence[j].CreatedAt) {
			return copy.Evidence[i].CreatedAt.Before(copy.Evidence[j].CreatedAt)
		}
		return copy.Evidence[i].CommentID < copy.Evidence[j].CommentID
	})
	encoded, err := json.Marshal(copy)
	if err != nil {
		return nil, fmt.Errorf("canonical serialize prompt context: %w", err)
	}
	return encoded, nil
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func boundedEvidence(state workflow.WorkUnitState, limit, chars int) []Evidence {
	mandatory := make(map[int64]struct{})
	for _, result := range []*workflow.ResultEvidence{state.LatestResults.Lead, state.LatestResults.Implementor, state.LatestResults.QA, state.ActiveBlocker} {
		if result != nil {
			mandatory[result.CommentID] = struct{}{}
		}
	}
	for index := len(state.ParsedComments) - 1; index >= 0; index-- {
		parsed := state.ParsedComments[index]
		if containsOperationalHeading(parsed.Markdown, "Lead Dispatch") ||
			containsOperationalHeading(parsed.Markdown, "Lead Decision") ||
			containsOperationalHeading(parsed.Markdown, "Lead Escalation") {
			mandatory[parsed.CommentID] = struct{}{}
			if len(mandatory) >= minimumSafeEvidenceLimit-1 {
				break
			}
		}
	}

	selected := make(map[int64]workflow.ParsedComment)
	for _, parsed := range state.ParsedComments {
		if _, ok := mandatory[parsed.CommentID]; ok {
			selected[parsed.CommentID] = parsed
		}
	}
	for index := len(state.ParsedComments) - 1; index >= 0 && len(selected) < limit; index-- {
		parsed := state.ParsedComments[index]
		if _, exists := selected[parsed.CommentID]; !exists {
			selected[parsed.CommentID] = parsed
		}
	}

	items := make([]Evidence, 0, len(selected))
	for _, parsed := range selected {
		markdown := truncateUTF8(redactText(parsed.Markdown), chars)
		items = append(items, Evidence{
			CommentID: parsed.CommentID,
			Author:    redactText(parsed.Author),
			URL:       redactText(parsed.URL),
			CreatedAt: parsed.CreatedAt,
			UpdatedAt: parsed.UpdatedAt,
			Heading:   redactText(parsed.Heading),
			Markdown:  markdown,
			Level:     parsed.Level,
			Event:     sanitizeEvent(parsed.Event),
			Warnings:  sanitizeWarnings(parsed.Warnings),
			HardError: sanitizeProtocolError(parsed.HardError),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].CommentID < items[j].CommentID
	})
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

func latestSummary(results workflow.LatestResults) LatestResultSummary {
	return LatestResultSummary{
		Lead:        resultSummary(results.Lead),
		Implementor: resultSummary(results.Implementor),
		QA:          resultSummary(results.QA),
	}
}

func resultSummary(result *workflow.ResultEvidence) *ResultSummary {
	if result == nil {
		return nil
	}
	return &ResultSummary{
		CommentID:  result.CommentID,
		Role:       result.Role,
		Status:     result.Status,
		Head:       result.Head,
		Verdict:    result.Verdict,
		Decision:   redactText(result.Decision),
		ResumeRole: result.ResumeRole,
		Resolves:   sanitizeRawJSON(result.Resolves),
		EscalateTo: result.EscalateTo,
		Stale:      result.Stale,
		Effective:  result.Effective,
		CreatedAt:  result.CreatedAt,
	}
}

func sanitizeCandidateState(state workflow.CandidateState) workflow.CandidateState {
	result := workflow.CandidateState{Ambiguous: state.Ambiguous, Alternatives: make([]workflow.Candidate, 0, len(state.Alternatives))}
	for _, candidate := range state.Alternatives {
		result.Alternatives = append(result.Alternatives, sanitizeCandidate(candidate))
	}
	sort.Slice(result.Alternatives, func(i, j int) bool { return result.Alternatives[i].Number < result.Alternatives[j].Number })
	if state.Current != nil {
		current := sanitizeCandidate(*state.Current)
		result.Current = &current
	}
	return result
}

func sanitizeCandidate(candidate workflow.Candidate) workflow.Candidate {
	candidate.Title = redactText(candidate.Title)
	candidate.URL = redactText(candidate.URL)
	candidate.CI = sanitizeCI(candidate.CI)
	return candidate
}

func sanitizeCI(ci supervisor.CISummary) supervisor.CISummary {
	ci.Source = redactText(ci.Source)
	ci.DetailsURL = redactText(ci.DetailsURL)
	return ci
}

func sanitizeRoute(route workflow.Route) workflow.Route {
	route.Reason = redactText(route.Reason)
	route.Guards = sortedUnique(route.Guards)
	route.Warnings = sanitizeWarnings(route.Warnings)
	return route
}

func redactAttention(attention workflow.Attention) workflow.Attention {
	attention.Explanation = redactText(attention.Explanation)
	return attention
}

func sanitizeWarnings(warnings []workflow.Warning) []workflow.Warning {
	result := make([]workflow.Warning, 0, len(warnings))
	seen := make(map[string]struct{}, len(warnings))
	for _, warning := range warnings {
		warning.Message = redactText(warning.Message)
		key := fmt.Sprintf("%d\x00%s\x00%s", warning.CommentID, warning.Code, warning.Message)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, warning)
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

func sanitizeEvent(event *workflow.WorkerEvent) *workflow.WorkerEvent {
	if event == nil {
		return nil
	}
	copy := *event
	copy.Decision = redactText(copy.Decision)
	copy.Resolves = sanitizeRawJSON(copy.Resolves)
	copy.Extensions = nil
	return &copy
}

func sanitizeProtocolError(value *workflow.ProtocolError) *workflow.ProtocolError {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Message = redactText(copy.Message)
	return &copy
}

func sanitizeRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return json.RawMessage(`"[REDACTED]"`)
	}
	value = sanitizeJSONValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`"[REDACTED]"`)
	}
	return json.RawMessage(encoded)
}

func sanitizeJSONValue(value any) any {
	switch typed := value.(type) {
	case string:
		return redactText(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = sanitizeJSONValue(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if credentialKey(key) {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = sanitizeJSONValue(item)
		}
		return result
	default:
		return value
	}
}

func credentialKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"authorization", "credential", "password", "secret", "token", "api_key", "apikey"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func expectedEvent(route workflow.Route) string {
	switch route.TargetRole {
	case "qa":
		return "qa verdict and terminal worker_result"
	case "lead", "implementor":
		return "terminal worker_result"
	default:
		return "none"
	}
}

func redactText(value string) string {
	value = authorizationPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = tokenPattern.ReplaceAllString(value, "[REDACTED]")
	value = assignmentPattern.ReplaceAllString(value, `${1}=[REDACTED]`)
	value = credentialValuePattern.ReplaceAllString(value, `${1}[REDACTED]`)
	return value
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value) + "\n…[truncated]"
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsOperationalHeading(markdown, expected string) bool {
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if strings.EqualFold(heading, expected) {
			return true
		}
	}
	return false
}
