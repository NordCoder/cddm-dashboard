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
	fullSHA      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	reasonCode   = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,119}$`)
	commandID    = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)
	blockerTypes = map[string]bool{"candidate": true, "process": true, "authority": true, "infrastructure": true}
)

func ParseMarker(body string) ParsedMarker {
	positions := markerPositions(body)
	if len(positions) == 0 {
		return ParsedMarker{}
	}
	parsed := ParsedMarker{Present: true}
	if len(positions) > 1 {
		parsed.Status = ValidationMalformed
		parsed.Reason = "multiple_markers"
		return parsed
	}

	start := positions[0]
	contentStart := start + len(resultMarker)
	endOffset := strings.Index(body[contentStart:], "-->")
	if endOffset < 0 {
		parsed.Status = ValidationMalformed
		parsed.Reason = "marker_not_closed"
		return parsed
	}
	jsonText := strings.TrimSpace(body[contentStart : contentStart+endOffset])
	if jsonText == "" {
		parsed.Status = ValidationMalformed
		parsed.Reason = "empty_marker"
		return parsed
	}

	decoder := json.NewDecoder(strings.NewReader(jsonText))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed.Payload); err != nil {
		parsed.Status = ValidationMalformed
		parsed.Reason = "invalid_json"
		return parsed
	}
	if err := requireJSONEOF(decoder); err != nil {
		parsed.Status = ValidationMalformed
		parsed.Reason = "invalid_json"
		return parsed
	}
	canonical, err := json.Marshal(parsed.Payload)
	if err != nil {
		parsed.Status = ValidationMalformed
		parsed.Reason = "invalid_payload"
		return parsed
	}
	parsed.JSON = canonical
	parsed.Hash = hashBytes(canonical)
	parsed.Status, parsed.Reason = validatePayload(parsed.Payload)
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

func validatePayload(payload MarkerPayload) (string, string) {
	if payload.Version != 1 {
		return ValidationUnsupported, "unsupported_version"
	}
	if !commandID.MatchString(payload.CommandID) {
		return ValidationMalformed, "invalid_command_id"
	}
	if payload.Role != "lead" && payload.Role != "implementor" && payload.Role != "qa" {
		return ValidationUnsupported, "unsupported_role"
	}

	switch payload.Role {
	case "implementor":
		return validateImplementor(payload)
	case "qa":
		return validateQA(payload)
	case "lead":
		return validateLead(payload)
	default:
		return ValidationUnsupported, "unsupported_role"
	}
}

func validateImplementor(payload MarkerPayload) (string, string) {
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

func validateQA(payload MarkerPayload) (string, string) {
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
		if payload.CycleEscalation != "none" && payload.CycleEscalation != "lead_cycle_review" && payload.CycleEscalation != "owner_exception" {
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

func validateLead(payload MarkerPayload) (string, string) {
	switch payload.Result {
	case "dispatch":
		if !validNextRole(payload.NextRole) {
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

func validNextRole(role string) bool {
	return role == "lead" || role == "implementor" || role == "qa"
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
			prefix := strings.TrimSpace(line[:index])
			if prefix == "" {
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
				inFence = true
				fenceCharacter = character
				fenceLength = length
			} else if character == fenceCharacter && length >= fenceLength {
				inFence = false
				fenceCharacter = 0
				fenceLength = 0
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
