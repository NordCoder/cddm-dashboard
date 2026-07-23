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

	markers := envelopeMarkerPositions(comment.Body)
	if len(markers) > 0 {
		parsed.Level = ParseLevelEnvelope
		parsed.Meaningful = true
		parseEnvelope(&parsed, comment.Body, markers[0], len(markers))
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

func parseEnvelope(parsed *ParsedComment, body string, start, markerCount int) {
	contentStart := start + len(envelopeMarker)
	endOffset := strings.Index(body[contentStart:], "-->")
	if endOffset < 0 {
		parsed.HardError = &ProtocolError{Code: "malformed_envelope", Message: "supervisor:event envelope is missing the closing --> marker"}
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, parsed.HardError.Code, parsed.HardError.Message))
		return
	}
	end := contentStart + endOffset
	if markerCount > 1 {
		parsed.Warnings = append(parsed.Warnings, warning(parsed.CommentID, "multiple_envelopes", "comment contains more than one live supervisor:event envelope; automatic transition is disabled"))
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
	if markerCount > 1 {
		transitionSafe = false
	}
	parsed.TransitionSafe = transitionSafe
}

func hasMarkdownHeading(body, expected string) bool {
	for _, line := range liveMarkdownLines(body) {
		heading, ok := atxHeading(line)
		if ok && strings.EqualFold(heading, expected) {
			return true
		}
	}
	return false
}

func classifyHeading(body string) (string, *WorkerEvent) {
	for _, line := range liveMarkdownLines(body) {
		heading, ok := atxHeading(line)
		if !ok {
			continue
		}
		for _, supported := range supportedHeadings {
			if strings.EqualFold(heading, supported.Name) {
				return supported.Name, &WorkerEvent{Version: 1, Event: "worker_result", Role: supported.Role, Status: supported.Status}
			}
		}
	}
	return "", nil
}

// envelopeMarkerPositions returns only operational envelopes: the marker must start
// its own non-code Markdown line. Fenced, indented and quoted examples remain
// human-readable activity and can never become authoritative workflow evidence.
func envelopeMarkerPositions(body string) []int {
	positions := make([]int, 0, 1)
	for _, line := range liveMarkdownLineSpans(body) {
		text := body[line.start:line.end]
		searchFrom := 0
		for {
			relative := strings.Index(text[searchFrom:], envelopeMarker)
			if relative < 0 {
				break
			}
			index := searchFrom + relative
			if strings.TrimSpace(text[:index]) == "" {
				positions = append(positions, line.start+index)
			}
			searchFrom = index + len(envelopeMarker)
		}
	}
	return positions
}

type markdownLineSpan struct {
	start int
	end   int
}

func liveMarkdownLines(body string) []string {
	spans := liveMarkdownLineSpans(body)
	lines := make([]string, 0, len(spans))
	for _, span := range spans {
		lines = append(lines, body[span.start:span.end])
	}
	return lines
}

func liveMarkdownLineSpans(body string) []markdownLineSpan {
	spans := make([]markdownLineSpan, 0)
	inFence := false
	var fenceCharacter byte
	fenceLength := 0
	for start := 0; start <= len(body); {
		end := strings.IndexByte(body[start:], '\n')
		if end < 0 {
			end = len(body)
		} else {
			end += start
		}
		line := strings.TrimSuffix(body[start:end], "\r")
		character, length, isFence := markdownFence(line)
		if isFence {
			if !inFence {
				inFence = true
				fenceCharacter = character
				fenceLength = length
			} else if character == fenceCharacter && length >= fenceLength {
				inFence = false
				fenceCharacter = 0
				fenceLength = 0
			}
		} else if !inFence && !markdownIndentedCode(line) {
			spans = append(spans, markdownLineSpan{start: start, end: start + len(line)})
		}
		if end == len(body) {
			break
		}
		start = end + 1
	}
	return spans
}

func markdownFence(line string) (byte, int, bool) {
	indent, contentStart := markdownLeadingIndent(line)
	if indent > 3 {
		return 0, 0, false
	}
	trimmed := line[contentStart:]
	if len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	character := trimmed[0]
	length := 0
	for length < len(trimmed) && trimmed[length] == character {
		length++
	}
	return character, length, length >= 3
}

func markdownIndentedCode(line string) bool {
	indent, _ := markdownLeadingIndent(line)
	return indent >= 4
}

func markdownLeadingIndent(line string) (int, int) {
	columns := 0
	index := 0
	for index < len(line) {
		switch line[index] {
		case ' ':
			columns++
			index++
		case '\t':
			columns += 4 - columns%4
			index++
		default:
			return columns, index
		}
	}
	return columns, index
}

func atxHeading(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	count := 0
	for count < len(trimmed) && count < 6 && trimmed[count] == '#' {
		count++
	}
	if count == 0 || (count < len(trimmed) && trimmed[count] != ' ' && trimmed[count] != '\t') {
		return "", false
	}
	heading := strings.TrimSpace(trimmed[count:])
	heading = strings.TrimSpace(strings.TrimRight(heading, "#"))
	if heading == "" {
		return "", false
	}
	return heading, true
}

func populateLegacyFields(event *WorkerEvent, body string) {
	liveBody := strings.Join(liveMarkdownLines(body), "\n")
	for _, match := range fieldLinePattern.FindAllStringSubmatch(liveBody, -1) {
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

	lower := strings.ToLower(liveBody)
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
