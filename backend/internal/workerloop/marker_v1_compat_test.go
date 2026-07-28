package workerloop

import "testing"

func TestV1KnownOptionalFieldsRetainLegacyAcceptance(t *testing.T) {
	parsed := parsePayload(t, `{
  "version":1,
  "role":"implementor",
  "result":"candidate_ready",
  "command_id":"cmd-v1-compat",
  "pr":150,
  "head":"`+testHead+`",
  "reason_code":"legacy_metadata"
}`)
	if parsed.Status != ValidationAccepted {
		t.Fatalf("legacy v1 marker changed semantics: %+v", parsed)
	}
}

func TestV2RejectsTheSameResultShape(t *testing.T) {
	parsed := parsePayload(t, `{
  "version":2,
  "role":"implementor",
  "result":"candidate_ready",
  "command_id":"cmd-v2-strict",
  "pr":150,
  "head":"`+testHead+`",
  "reason_code":"legacy_metadata"
}`)
	if parsed.Status != ValidationMalformed || parsed.Reason != "candidate_identity_required" {
		t.Fatalf("v2 accepted a result-specific extra field: %+v", parsed)
	}
}
