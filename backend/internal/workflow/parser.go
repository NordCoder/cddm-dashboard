package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

const envelopeMarker = "<!-- supervisor:event"

var fieldLinePattern = regexp.MustCompile(`(?im)^\s*(?:[-*]\s*)?(head|verdict|decision|resume[_ ]role|resolves|escalate[_ ]to)\s*:\s*(.+?)\s*$`)

var supportedHeadings = []struct {
	Name   string
	Role   string
	Status string
}{
	{Name: "Implementor Handoff", Role: "implementor", Status: "completed"},
	{Name: "Implementor Result", Role: "implementor", Status: "completed"},
	{Name: "QA Verdict", Role: "qa", Status: "completed"},
	{Name: "Blocker", Role: "", Status: "blocked"},
	{Name: "Lead Decision", Role: "lead", Status: "completed"},
	{Name: "Lead Escalation", Role: "lead", Status: "blocked"},
	{Name: "Stage Handoff", Role: "lead", Status: "completed"},
}

func ParseComment(projectID int64, issueNumber int, comment supervisor.Comment) ParsedComment {
	parsed := ParsedComment{
		ProjectID:   projectID,
		IssueNumber: issueNumber,
		CommentID:   comment.GitHubID,
		Author:      comment.Author,
		URL:         comment.URL,
		CreatedAt:   comment.CreatedAt,
		UpdatedAt:   comment.UpdatedAt,
		Markdown:    strings.TrimSpace(comment.Body),
		Warnings:    make([]Warning, 0),
	}

	start := strings.Index(comment.Body, envelopeMarker)
	if start >= 0 {
		parsed.Level = ParseLevelEnvelope
		parsed.Meaningful = true
		parseEnvelope(&parsed, comment.Body, start)
		return parsed
	}

	if heading, event := classifyHeading(comment.Body); event != nil {
		parsed.Level = ParseLevelHeading
		parsed.Heading = heading
		parsed.Meaningful = true
		parsed.Event = event
		parsed.Warnings = append(parsed.Warnings, warning(comment.GitHubID, "legacy_heading", "comment was classified from a Markdown heading rather than a supervisor:event envelope"))
		populateLegacyFields(event, comment.Body)
		parsed.TransitionSafe = legacyTransitionSafe(event)
		if !parsed.TransitionSafe {
			parsed.Warnings = append(parsed.Warnings, warning(comment.GitHubID, "legacy_result_incomplete", "legacy terminal result lacks enough exact data for an automatic transition"))
		}
		return parsed
	}

	if strings.TrimSpace(comment.Body) == "" {
		parsed.Level = ParseLevelNoActivity
		parsed.Meaningful = false
		return parsed
	}

	parsed.Level = ParseLevelActivity
	parsed.Meaningful = true
	return parsed
}

func parseEnvelope(parsed *ParsedComment, body string, start int) {
	contentStart := start + len(envelopeMarker)
	endOffset := strings.Index(body[contentStart:], "-->")
	if endOffset < 0 {
		parsed.HardError = &ProtocolError{Code: "malformed_envelope", Message: "supervisor:event envelope is missing the closing --> marker"}
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, parsed.HardError.Code, parsed.HardError.Message))
		return
	}
	end := contentStart + endOffset
	if strings.Contains(body[end+3:], envelopeMarker) {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "multiple_envelopes", "comment contains more than one supervisor:event envelope; automatic transition is disabled"))
	}

	jsonText := strings.TrimSpace(body[contentStart:end])
	parsed.Markdown = strings.TrimSpace(body[:start] + body[end+3:])
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		parsed.HardError = &ProtocolError{Code: "malformed_envelope", Message: fmt.Sprintf("decode supervisor:event JSON: %v", err)}
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, parsed.HardError.Code, parsed.HardError.Message))
		return
	}

	event := &WorkerEvent{Extensions: make(map[string]json.RawMessage)}
	missing := make([]string, 0)
	if !decodeInt(raw, "v", &event.Version) {
		missing = append(missing, "v")
	}
	if !decodeString(raw, "event", &event.Event) {
		missing = append(missing, "event")
	}
	if !decodeString(raw, "role", &event.Role) {
		missing = append(missing, "role")
	}
	if !decodeString(raw, "status", &event.Status) {
		missing = append(missing, "status")
	}
	decodeString(raw, "head", &event.Head)
	decodeString(raw, "verdict", &event.Verdict)
	decodeString(raw, "decision", &event.Decision)
	decodeString(raw, "resume_role", &event.ResumeRole)
	decodeString(raw, "escalate_to", &event.EscalateTo)
	if value, ok := raw["resolves"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		event.Resolves = append(json.RawMessage(nil), value...)
	}

	known := map[string]struct{}{
		"v": {}, "event": {}, "role": {}, "status": {}, "head": {}, "verdict": {},
		"decision": {}, "resume_role": {}, "resolves": {}, "escalate_to": {},
	}
	for key, value := range raw {
		if _, ok := known[key]; !ok {
			event.Extensions[key] = append(json.RawMessage(nil), value...)
		}
	}
	if len(event.Extensions) == 0 {
		event.Extensions = nil
	}
	parsed.Event = event

	if event.Role == "qa" {
		if strings.TrimSpace(event.Head) == "" {
			missing = append(missing, "head")
		}
		if strings.TrimSpace(event.Verdict) == "" {
			missing = append(missing, "verdict")
		}
	}
	if len(missing) > 0 {
		parsed.HardError = &ProtocolError{Code: "missing_required_field", Message: "supervisor:event is missing required field(s): " + strings.Join(uniqueStrings(missing), ", ")}
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, parsed.HardError.Code, parsed.HardError.Message))
		return
	}

	transitionSafe := true
	if event.Version != 1 {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "unknown_version", fmt.Sprintf("event version %d is not supported for automatic transition", event.Version)))
		transitionSafe = false
	}
	if event.Event != "worker_result" {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "unknown_event", fmt.Sprintf("event %q is preserved but not used for automatic transition", event.Event)))
		transitionSafe = false
	}
	if !oneOf(event.Role, "lead", "implementor", "qa") {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "unknown_role", fmt.Sprintf("role %q is preserved but not routable", event.Role)))
		transitionSafe = false
	}
	if !oneOf(event.Status, "completed", "no_op", "blocked") {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "unknown_status", fmt.Sprintf("status %q is preserved but not routable", event.Status)))
		transitionSafe = false
	}
	if event.Role == "qa" && !oneOf(event.Verdict, "approved", "changes_required", "inconclusive") {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "unknown_verdict", fmt.Sprintf("QA verdict %q is preserved but not routable", event.Verdict)))
		transitionSafe = false
	}
	if event.ResumeRole != "" && !oneOf(event.ResumeRole, "lead", "implementor", "qa") {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "unknown_resume_role", fmt.Sprintf("resume_role %q requires Lead/manual attention", event.ResumeRole)))
		transitionSafe = false
	}
	if event.EscalateTo != "" && event.EscalateTo != "owner" {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "unknown_escalation_target", fmt.Sprintf("escalate_to %q is preserved but not automatically routed", event.EscalateTo)))
		transitionSafe = false
	}
	if heading, headingEvent := classifyHeading(body[:start]); headingEvent != nil && headingEvent.Role != "" && headingEvent.Role != event.Role {
		parsed.Heading = heading
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "role_mismatch", fmt.Sprintf("Markdown heading implies role %q but envelope role is %q", headingEvent.Role, event.Role)))
		transitionSafe = false
	}
	if strings.Contains(body[end+3:], envelopeMarker) {
		transitionSafe = false
	}
	parsed.TransitionSafe = transitionSafe
}

func hasMarkdownHeading(body, expected string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if strings.EqualFold(trimmed, expected) {
			return true
		}
	}
	return false
}

func classifyHeading(body string) (string, *WorkerEvent) {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		for _, supported := range supportedHeadings {
			if strings.EqualFold(trimmed, supported.Name) {
				return supported.Name, &WorkerEvent{Version: 1, Event: "worker_result", Role: supported.Role, Status: supported.Status}
			}
		}
	}
	return "", nil
}

func populateLegacyFields(event *WorkerEvent, body string) {
	for _, match := range fieldLinePattern.FindAllStringSubmatch(body, -1) {
		key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(match[1]), " ", "_"))
		value := strings.Trim(strings.TrimSpace(match[2]), "`\"")
		switch key {
		case "head":
			event.Head = value
		case "verdict":
			event.Verdict = normalizeToken(value)
		case "decision":
			event.Decision = normalizeToken(value)
		case "resume_role":
			event.ResumeRole = normalizeToken(value)
		case "resolves":
			event.Resolves = json.RawMessage(strconv.Quote(value))
		case "escalate_to":
			event.EscalateTo = normalizeToken(value)
		}
	}

	lower := strings.ToLower(body)
	if event.Role == "qa" && event.Verdict == "" {
		for _, verdict := range []string{"changes_required", "inconclusive", "approved"} {
			if strings.Contains(strings.ReplaceAll(lower, " ", "_"), verdict) {
				event.Verdict = verdict
				break
			}
		}
	}
	if event.Role == "" {
		switch {
		case strings.Contains(lower, "implementor"):
			event.Role = "implementor"
		case strings.Contains(lower, "qa"):
			event.Role = "qa"
		case strings.Contains(lower, "lead"):
			event.Role = "lead"
		}
	}
	if event.EscalateTo == "" && strings.EqualFold(strings.TrimSpace(event.Decision), "owner_required") {
		event.EscalateTo = "owner"
	}
	if strings.Contains(lower, "owner_required") || strings.Contains(lower, "escalate_to: owner") || strings.Contains(lower, "escalate to: owner") {
		event.EscalateTo = "owner"
	}
}

func legacyTransitionSafe(event *WorkerEvent) bool {
	if event == nil || !oneOf(event.Role, "lead", "implementor", "qa") || !oneOf(event.Status, "completed", "no_op", "blocked") {
		return false
	}
	if event.Role == "qa" {
		return event.Head != "" && oneOf(event.Verdict, "approved", "changes_required", "inconclusive")
	}
	if event.Role == "lead" && event.EscalateTo != "" {
		return event.EscalateTo == "owner"
	}
	if event.Role == "lead" && event.ResumeRole != "" {
		return oneOf(event.ResumeRole, "lead", "implementor", "qa")
	}
	return true
}

func decodeString(raw map[string]json.RawMessage, key string, destination *string) bool {
	value, ok := raw[key]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return false
	}
	if err := json.Unmarshal(value, destination); err != nil || strings.TrimSpace(*destination) == "" {
		return false
	}
	*destination = strings.TrimSpace(*destination)
	return true
}

func decodeInt(raw map[string]json.RawMessage, key string, destination *int) bool {
	value, ok := raw[key]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return false
	}
	return json.Unmarshal(value, destination) == nil
}

func warning(commentID int64, code, message string) Warning {
	return Warning{Code: code, Message: message, CommentID: commentID}
}

func normalizeToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "_", " ", "_").Replace(value)
	return value
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
