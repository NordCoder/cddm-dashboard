package planning

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

const maxPlanResponseBytes = 256 << 10

var requiredPlanFields = []string{
	"v", "action", "target_role", "lane_key", "summary", "reason", "risk", "requires_owner",
	"expected_head", "expected_event", "guards", "prohibited_actions", "prompt", "confidence", "source",
}

func ParsePlan(output string) (PromptPlan, []byte, error) {
	if len(output) > maxPlanResponseBytes {
		return PromptPlan{}, nil, fmt.Errorf("structured response exceeds %d bytes", maxPlanResponseBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.UseNumber()
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return PromptPlan{}, nil, fmt.Errorf("malformed JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errorsIsEOF(err) {
		return PromptPlan{}, nil, fmt.Errorf("structured response must contain exactly one JSON object")
	}
	for _, field := range requiredPlanFields {
		if _, ok := raw[field]; !ok {
			return PromptPlan{}, nil, fmt.Errorf("missing required field %q", field)
		}
	}

	known := map[string]struct{}{
		"v": {}, "action": {}, "target_role": {}, "lane_key": {}, "summary": {}, "reason": {},
		"risk": {}, "requires_owner": {}, "expected_head": {}, "expected_event": {}, "guards": {},
		"prohibited_actions": {}, "prompt": {}, "confidence": {}, "source": {}, "extensions": {},
	}
	var plan PromptPlan
	encoded, err := json.Marshal(raw)
	if err != nil {
		return PromptPlan{}, nil, fmt.Errorf("normalize plan JSON: %w", err)
	}
	if err := json.Unmarshal(encoded, &plan); err != nil {
		return PromptPlan{}, nil, fmt.Errorf("decode PromptPlan: %w", err)
	}
	filteredExtensions := make(map[string]json.RawMessage)
	for key, value := range plan.Extensions {
		if safeExtension(value) {
			filteredExtensions[key] = append(json.RawMessage(nil), value...)
		}
	}
	plan.Extensions = filteredExtensions
	for key, value := range raw {
		if _, ok := known[key]; ok {
			continue
		}
		if safeExtension(value) {
			plan.Extensions[key] = append(json.RawMessage(nil), value...)
		}
	}
	if len(plan.Extensions) == 0 {
		plan.Extensions = nil
	}
	canonical, err := CanonicalPlanBytes(plan)
	if err != nil {
		return PromptPlan{}, nil, err
	}
	return plan, canonical, nil
}

func CanonicalPlanBytes(plan PromptPlan) ([]byte, error) {
	plan.Guards = sortedUnique(plan.Guards)
	plan.ProhibitedActions = sortedUnique(plan.ProhibitedActions)
	if len(plan.Extensions) > 0 {
		keys := make([]string, 0, len(plan.Extensions))
		for key := range plan.Extensions {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ordered := make(map[string]json.RawMessage, len(keys))
		for _, key := range keys {
			ordered[key] = plan.Extensions[key]
		}
		plan.Extensions = ordered
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("canonical serialize PromptPlan: %w", err)
	}
	return encoded, nil
}

func safeExtension(value json.RawMessage) bool {
	if len(value) > 16<<10 || !json.Valid(value) {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(value)))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return false
	}
	return !containsCredentialMaterial(decoded)
}

func containsCredentialMaterial(value any) bool {
	switch typed := value.(type) {
	case string:
		return redactText(typed) != typed
	case []any:
		for _, item := range typed {
			if containsCredentialMaterial(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if credentialKey(key) || containsCredentialMaterial(item) {
				return true
			}
		}
	}
	return false
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}
