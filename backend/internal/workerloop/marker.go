package workerloop

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const resultMarker = "<!-- cddm-dashboard:result"

var (
	fullSHA        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	reasonCode     = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,119}$`)
	commandID      = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)
	actionID       = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,120}$`)
	waveID         = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,120}$`)
	repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	blockerTypes   = map[string]bool{"candidate": true, "process": true, "authority": true, "infrastructure": true}
	decisionTypes  = map[string]bool{
		"product_behavior": true, "scope": true, "architecture": true, "visual_acceptance": true,
		"security_privacy_legal": true, "release": true, "residual_risk": true, "product_envelope": true,
	}
)

func ParseMarker(body string) ParsedMarker {
	positions := markerPositions(body)
	if len(positions) == 0 {
		return ParsedMarker{}
	}
	parsed := ParsedMarker{Present: true}
	if len(positions) > 1 {
		parsed.Status, parsed.Reason = ValidationMalformed, "multiple_markers"
		return parsed
	}

	start := positions[0]
	contentStart := start + len(resultMarker)
	endOffset := strings.Index(body[contentStart:], "-->")
	if endOffset < 0 {
		parsed.Status, parsed.Reason = ValidationMalformed, "marker_not_closed"
		return parsed
	}
	jsonText := strings.TrimSpace(body[contentStart : contentStart+endOffset])
	if jsonText == "" {
		parsed.Status, parsed.Reason = ValidationMalformed, "empty_marker"
		return parsed
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonText), &fields); err != nil || fields == nil {
		parsed.Status, parsed.Reason = ValidationMalformed, "invalid_json"
		return parsed
	}
	decoder := json.NewDecoder(strings.NewReader(jsonText))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed.Payload); err != nil {
		parsed.Status, parsed.Reason = ValidationMalformed, "invalid_json"
		return parsed
	}
	if err := requireJSONEOF(decoder); err != nil {
		parsed.Status, parsed.Reason = ValidationMalformed, "invalid_json"
		return parsed
	}
	canonical, err := json.Marshal(parsed.Payload)
	if err != nil {
		parsed.Status, parsed.Reason = ValidationMalformed, "invalid_payload"
		return parsed
	}
	parsed.JSON = canonical
	parsed.Hash = hashBytes(canonical)
	parsed.Status, parsed.Reason = validatePayload(parsed.Payload, fields)
	return parsed
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("additional JSON value")
}

func validatePayload(payload MarkerPayload, fields map[string]json.RawMessage) (string, string) {
	if payload.Version != 1 && payload.Version != 2 {
		return ValidationUnsupported, "unsupported_version"
	}
	if !commandID.MatchString(payload.CommandID) {
		return ValidationMalformed, "invalid_command_id"
	}
	if !validRole(payload.Role) {
		return ValidationUnsupported, "unsupported_role"
	}
	if payload.Version == 1 {
		return validateV1(payload, fields)
	}
	return validateV2(payload, fields)
}

// validateV1 preserves the original cddm-worker-result/v1 semantic validator.
// Existing known optional fields remain accepted exactly as before. Only fields
// introduced exclusively for v2 are rejected so a v1 result cannot smuggle a
// typed action batch or merge claim into a legacy command.
func validateV1(payload MarkerPayload, fields map[string]json.RawMessage) (string, string) {
	if hasAnyField(fields, "repository", "issue", "merge_commit", "actions", "wave") {
		return ValidationMalformed, "v2_fields_not_allowed"
	}
	switch payload.Role {
	case "implementor":
		return validateImplementorV1(payload)
	case "qa":
		return validateQAV1(payload)
	case "lead":
		return validateLeadV1(payload)
	default:
		return ValidationUnsupported, "unsupported_role"
	}
}

func validateImplementorV1(payload MarkerPayload) (string, string) {
	switch payload.Result {
	case "candidate_ready":
		if payload.PR <= 0 || !fullSHA.MatchString(payload.Head) {
			return ValidationMalformed, "candidate_identity_required"
		}
	case "continue":
	case "blocked":
		if !blockerTypes[payload.BlockerType] || !reasonCode.MatchString(payload.ReasonCode) {
			return ValidationMalformed, "blocker_identity_required"
		}
	case "no_op":
		if !reasonCode.MatchString(payload.ReasonCode) {
			return ValidationMalformed, "reason_code_required"
		}
	default:
		return ValidationUnsupported, "unsupported_result"
	}
	return ValidationAccepted, ""
}

func validateQAV1(payload MarkerPayload) (string, string) {
	if !fullSHA.MatchString(payload.ReviewedHead) || payload.BlockingFindings == nil {
		return ValidationMalformed, "qa_identity_required"
	}
	switch payload.Result {
	case "approved":
		if *payload.BlockingFindings != 0 {
			return ValidationMalformed, "approved_requires_zero_findings"
		}
	case "changes_required":
		if *payload.BlockingFindings <= 0 {
			return ValidationMalformed, "changes_required_needs_findings"
		}
		if !validCycleEscalation(payload.CycleEscalation) {
			return ValidationMalformed, "cycle_escalation_required"
		}
	case "blocked_inconclusive":
		if *payload.BlockingFindings != 0 || !blockerTypes[payload.BlockerType] || !reasonCode.MatchString(payload.ReasonCode) {
			return ValidationMalformed, "inconclusive_blocker_required"
		}
	default:
		return ValidationUnsupported, "unsupported_result"
	}
	return ValidationAccepted, ""
}

func validateLeadV1(payload MarkerPayload) (string, string) {
	switch payload.Result {
	case "dispatch":
		if !validRole(payload.NextRole) {
			return ValidationMalformed, "next_role_required"
		}
	case "continue":
	case "correct":
		if payload.NextRole != "implementor" {
			return ValidationMalformed, "implementor_next_role_required"
		}
	case "ready_to_merge":
		if payload.PR <= 0 || !fullSHA.MatchString(payload.ApprovedHead) {
			return ValidationMalformed, "merge_identity_required"
		}
	case "owner_required", "hold":
		if !reasonCode.MatchString(payload.ReasonCode) {
			return ValidationMalformed, "reason_code_required"
		}
	default:
		return ValidationUnsupported, "unsupported_result"
	}
	return ValidationAccepted, ""
}

func validateV2(payload MarkerPayload, fields map[string]json.RawMessage) (string, string) {
	if hasAnyField(fields, "next_role") {
		return ValidationMalformed, "v1_fields_not_allowed"
	}
	switch payload.Role {
	case "implementor":
		return validateImplementorV2(payload, fields)
	case "qa":
		return validateQAV2(payload, fields)
	case "lead":
		return validateLeadV2(payload, fields)
	default:
		return ValidationUnsupported, "unsupported_role"
	}
}

func validateImplementorV2(payload MarkerPayload, fields map[string]json.RawMessage) (string, string) {
	switch payload.Result {
	case "candidate_ready":
		if !onlyFields(fields, "version", "role", "result", "command_id", "pr", "head") || payload.PR <= 0 || !fullSHA.MatchString(payload.Head) {
			return ValidationMalformed, "candidate_identity_required"
		}
	case "continue":
		if !onlyFields(fields, "version", "role", "result", "command_id") {
			return ValidationMalformed, "unexpected_result_fields"
		}
	case "blocked":
		if !onlyFields(fields, "version", "role", "result", "command_id", "blocker_type", "reason_code") || !blockerTypes[payload.BlockerType] || !reasonCode.MatchString(payload.ReasonCode) {
			return ValidationMalformed, "blocker_identity_required"
		}
	case "no_op":
		if !onlyFields(fields, "version", "role", "result", "command_id", "reason_code") || !reasonCode.MatchString(payload.ReasonCode) {
			return ValidationMalformed, "reason_code_required"
		}
	default:
		return ValidationUnsupported, "unsupported_result"
	}
	return ValidationAccepted, ""
}

func validateQAV2(payload MarkerPayload, fields map[string]json.RawMessage) (string, string) {
	if !fullSHA.MatchString(payload.ReviewedHead) || payload.BlockingFindings == nil {
		return ValidationMalformed, "qa_identity_required"
	}
	switch payload.Result {
	case "approved":
		if !onlyFields(fields, "version", "role", "result", "command_id", "reviewed_head", "blocking_findings") || *payload.BlockingFindings != 0 {
			return ValidationMalformed, "approved_requires_zero_findings"
		}
	case "changes_required":
		if !onlyFields(fields, "version", "role", "result", "command_id", "reviewed_head", "blocking_findings", "cycle_escalation") || *payload.BlockingFindings <= 0 || !validCycleEscalation(payload.CycleEscalation) {
			return ValidationMalformed, "changes_required_identity_invalid"
		}
	case "blocked", "inconclusive":
		if !onlyFields(fields, "version", "role", "result", "command_id", "reviewed_head", "blocking_findings", "blocker_type", "reason_code") || *payload.BlockingFindings != 0 || !blockerTypes[payload.BlockerType] || !reasonCode.MatchString(payload.ReasonCode) {
			return ValidationMalformed, "inconclusive_blocker_required"
		}
	default:
		return ValidationUnsupported, "unsupported_result"
	}
	return ValidationAccepted, ""
}

func validateLeadV2(payload MarkerPayload, fields map[string]json.RawMessage) (string, string) {
	switch payload.Result {
	case "actions_ready":
		if !onlyFields(fields, "version", "role", "result", "command_id", "actions", "wave") || len(payload.Actions) == 0 || len(payload.Actions) > 100 {
			return ValidationMalformed, "actions_required"
		}
		return validateActions(payload, fields)
	case "merged":
		if !onlyFields(fields, "version", "role", "result", "command_id", "repository", "issue", "pr", "approved_head", "merge_commit") || !validRepository(payload.Repository) || payload.Issue <= 0 || payload.PR <= 0 || !fullSHA.MatchString(payload.ApprovedHead) || !fullSHA.MatchString(payload.MergeCommit) {
			return ValidationMalformed, "merged_identity_required"
		}
	case "hold", "owner_required":
		if !onlyFields(fields, "version", "role", "result", "command_id", "reason_code") || !reasonCode.MatchString(payload.ReasonCode) {
			return ValidationMalformed, "reason_code_required"
		}
	default:
		return ValidationUnsupported, "unsupported_result"
	}
	return ValidationAccepted, ""
}

func validateActions(payload MarkerPayload, fields map[string]json.RawMessage) (string, string) {
	var rawActions []map[string]json.RawMessage
	if err := json.Unmarshal(fields["actions"], &rawActions); err != nil || len(rawActions) != len(payload.Actions) {
		return ValidationMalformed, "actions_invalid"
	}
	seenIDs := make(map[string]bool, len(payload.Actions))
	repositories := make(map[string]bool)
	dispatched := make(map[int]bool)
	for index, action := range payload.Actions {
		if !actionID.MatchString(action.ActionID) || seenIDs[action.ActionID] {
			return ValidationMalformed, "duplicate_or_invalid_action_id"
		}
		seenIDs[action.ActionID] = true
		if !validRepository(action.Repository) {
			return ValidationMalformed, "action_repository_invalid"
		}
		repositories[action.Repository] = true
		status, reason := validateAction(action, rawActions[index])
		if status != ValidationAccepted {
			return status, reason
		}
		if action.Type == "dispatch" {
			dispatched[action.Issue] = true
		}
	}
	if len(repositories) != 1 {
		return ValidationMalformed, "action_repository_ambiguous"
	}
	if payload.Wave == nil {
		if hasAnyField(fields, "wave") {
			return ValidationMalformed, "wave_invalid"
		}
		return ValidationAccepted, ""
	}
	return validateWave(*payload.Wave, payload.Actions, dispatched)
}

func validateAction(action ActionPayload, fields map[string]json.RawMessage) (string, string) {
	switch action.Type {
	case "dispatch":
		if !onlyFields(fields, "action_id", "type", "repository", "issue", "role", "expected_head") || action.Issue <= 0 || !validRole(action.Role) {
			return ValidationMalformed, "dispatch_action_invalid"
		}
		if action.ExpectedHead != "" && !fullSHA.MatchString(action.ExpectedHead) {
			return ValidationMalformed, "dispatch_action_invalid"
		}
		if action.Role == "qa" && action.ExpectedHead == "" {
			return ValidationMalformed, "dispatch_action_invalid"
		}
	case "correct":
		if !onlyFields(fields, "action_id", "type", "repository", "issue", "role", "expected_previous_head") || action.Issue <= 0 || action.Role != "implementor" || (action.ExpectedPreviousHead != "" && !fullSHA.MatchString(action.ExpectedPreviousHead)) {
			return ValidationMalformed, "correct_action_invalid"
		}
	case "plan_next_wave":
		if !onlyFields(fields, "action_id", "type", "repository", "issue", "role") || action.Issue <= 0 || action.Role != "lead" {
			return ValidationMalformed, "plan_action_invalid"
		}
	case "merge_candidate":
		if !onlyFields(fields, "action_id", "type", "repository", "issue", "role", "pr", "expected_head") || action.Issue <= 0 || action.Role != "lead" || action.PR <= 0 || !fullSHA.MatchString(action.ExpectedHead) {
			return ValidationMalformed, "merge_action_invalid"
		}
	case "hold":
		if !onlyFields(fields, "action_id", "type", "repository", "issue", "reason_code") || action.Issue < 0 || !reasonCode.MatchString(action.ReasonCode) {
			return ValidationMalformed, "hold_action_invalid"
		}
	case "owner_required":
		if !onlyFields(fields, "action_id", "type", "repository", "issue", "reason_code", "decision_category") || action.Issue < 0 || !reasonCode.MatchString(action.ReasonCode) || !decisionTypes[action.DecisionCategory] {
			return ValidationMalformed, "owner_action_invalid"
		}
	default:
		return ValidationUnsupported, "unsupported_action"
	}
	return ValidationAccepted, ""
}

func validateWave(wave WavePayload, actions []ActionPayload, dispatched map[int]bool) (string, string) {
	if !waveID.MatchString(wave.WaveID) || wave.ControlIssue <= 0 || len(wave.Issues) == 0 || len(wave.Issues) > 100 {
		return ValidationMalformed, "wave_identity_invalid"
	}
	members := make(map[int]bool, len(wave.Issues))
	for _, issue := range wave.Issues {
		if issue <= 0 || members[issue] {
			return ValidationMalformed, "wave_membership_invalid"
		}
		members[issue] = true
		if !dispatched[issue] {
			return ValidationMalformed, "wave_dispatch_missing"
		}
	}
	for _, action := range actions {
		if action.Type == "plan_next_wave" || (action.Type == "hold" && action.Issue == 0) || (action.Type == "owner_required" && action.Issue == 0) {
			continue
		}
		if action.Issue > 0 && !members[action.Issue] {
			return ValidationMalformed, "action_outside_wave"
		}
	}
	return ValidationAccepted, ""
}

func validCycleEscalation(value string) bool {
	return value == "none" || value == "lead_cycle_review" || value == "owner_exception"
}

func validRole(role string) bool {
	return role == "lead" || role == "implementor" || role == "qa"
}

func validRepository(value string) bool {
	return len(value) <= 200 && repositoryName.MatchString(value)
}

func onlyFields(fields map[string]json.RawMessage, allowed ...string) bool {
	set := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		set[name] = true
	}
	for name := range fields {
		if !set[name] {
			return false
		}
	}
	return true
}

func hasAnyField(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

type lineSpan struct {
	start int
	end   int
}

func markerPositions(body string) []int {
	positions := make([]int, 0, 1)
	for _, span := range liveLineSpans(body) {
		line := body[span.start:span.end]
		searchFrom := 0
		for {
			relative := strings.Index(line[searchFrom:], resultMarker)
			if relative < 0 {
				break
			}
			index := searchFrom + relative
			if strings.TrimSpace(line[:index]) == "" {
				positions = append(positions, span.start+index)
			}
			searchFrom = index + len(resultMarker)
		}
	}
	return positions
}

func liveLineSpans(body string) []lineSpan {
	spans := make([]lineSpan, 0)
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
				inFence, fenceCharacter, fenceLength = true, character, length
			} else if character == fenceCharacter && length >= fenceLength {
				inFence, fenceCharacter, fenceLength = false, 0, 0
			}
		} else if !inFence && !markdownIndentedCode(line) && !strings.HasPrefix(strings.TrimSpace(line), ">") {
			spans = append(spans, lineSpan{start: start, end: start + len(line)})
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

func decodeCanonicalMarker(value []byte) (MarkerPayload, error) {
	var payload MarkerPayload
	decoder := json.NewDecoder(bufio.NewReader(bytes.NewReader(value)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return MarkerPayload{}, err
	}
	return payload, nil
}
