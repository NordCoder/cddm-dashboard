package workerloop

import "testing"

const testHead = "241401d9f5c1fb2004eeb19ec612323f74b57199"
const otherHead = "341401d9f5c1fb2004eeb19ec612323f74b57199"

func TestParseMarkerV1CandidateReadyCompatibility(t *testing.T) {
	parsed := parsePayload(t, `{"version":1,"role":"implementor","result":"candidate_ready","command_id":"cmd-1","pr":150,"head":"`+testHead+`"}`)
	if parsed.Status != ValidationAccepted || parsed.Payload.PR != 150 || parsed.Payload.Head != testHead || parsed.Hash == "" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerV2CandidateReady(t *testing.T) {
	parsed := parsePayload(t, `{"version":2,"role":"implementor","result":"candidate_ready","command_id":"cmd-2","pr":151,"head":"`+testHead+`"}`)
	if parsed.Status != ValidationAccepted || parsed.Payload.Version != 2 {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerV2LeadActionsAndWave(t *testing.T) {
	parsed := parsePayload(t, `{
  "version":2,
  "role":"lead",
  "result":"actions_ready",
  "command_id":"cmd-lead-wave",
  "actions":[
    {"action_id":"a-1","type":"dispatch","repository":"NordCoder/app","issue":101,"role":"implementor"},
    {"action_id":"a-2","type":"dispatch","repository":"NordCoder/app","issue":102,"role":"qa","expected_head":"`+testHead+`"}
  ],
  "wave":{"wave_id":"wave-1","control_issue":90,"issues":[101,102]}
}`)
	if parsed.Status != ValidationAccepted || len(parsed.Payload.Actions) != 2 || parsed.Payload.Wave == nil {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerV2LeadMerged(t *testing.T) {
	parsed := parsePayload(t, `{
  "version":2,"role":"lead","result":"merged","command_id":"cmd-merge",
  "repository":"NordCoder/app","issue":101,"pr":120,
  "approved_head":"`+testHead+`","merge_commit":"`+otherHead+`"
}`)
	if parsed.Status != ValidationAccepted || parsed.Payload.MergeCommit != otherHead {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerV2RejectsDuplicateActionIDs(t *testing.T) {
	parsed := parsePayload(t, `{
  "version":2,"role":"lead","result":"actions_ready","command_id":"cmd-actions",
  "actions":[
    {"action_id":"same","type":"dispatch","repository":"NordCoder/app","issue":101,"role":"implementor"},
    {"action_id":"same","type":"dispatch","repository":"NordCoder/app","issue":102,"role":"implementor"}
  ]
}`)
	if parsed.Status != ValidationMalformed || parsed.Reason != "duplicate_or_invalid_action_id" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerV2RejectsRoleActionMismatch(t *testing.T) {
	parsed := parsePayload(t, `{
  "version":2,"role":"lead","result":"actions_ready","command_id":"cmd-actions",
  "actions":[{"action_id":"a-1","type":"correct","repository":"NordCoder/app","issue":101,"role":"qa"}]
}`)
	if parsed.Status != ValidationMalformed || parsed.Reason != "correct_action_invalid" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerV2RejectsWaveWithoutDispatch(t *testing.T) {
	parsed := parsePayload(t, `{
  "version":2,"role":"lead","result":"actions_ready","command_id":"cmd-actions",
  "actions":[{"action_id":"a-1","type":"hold","repository":"NordCoder/app","issue":101,"reason_code":"blocked"}],
  "wave":{"wave_id":"wave-1","control_issue":90,"issues":[101]}
}`)
	if parsed.Status != ValidationMalformed || parsed.Reason != "wave_dispatch_missing" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerV2RejectsMixedRepositories(t *testing.T) {
	parsed := parsePayload(t, `{
  "version":2,"role":"lead","result":"actions_ready","command_id":"cmd-actions",
  "actions":[
    {"action_id":"a-1","type":"dispatch","repository":"NordCoder/app","issue":101,"role":"implementor"},
    {"action_id":"a-2","type":"dispatch","repository":"Other/app","issue":102,"role":"implementor"}
  ]
}`)
	if parsed.Status != ValidationMalformed || parsed.Reason != "action_repository_ambiguous" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerV1RejectsV2Fields(t *testing.T) {
	parsed := parsePayload(t, `{"version":1,"role":"lead","result":"hold","command_id":"cmd-1","reason_code":"wait","repository":"NordCoder/app"}`)
	if parsed.Status != ValidationMalformed || parsed.Reason != "v2_fields_not_allowed" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerMalformedJSON(t *testing.T) {
	parsed := ParseMarker("<!-- cddm-dashboard:result\n{broken}\n-->")
	if parsed.Status != ValidationMalformed || parsed.Reason != "invalid_json" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerRejectsTrailingJSON(t *testing.T) {
	parsed := ParseMarker("<!-- cddm-dashboard:result\n{\"version\":1,\"role\":\"implementor\",\"result\":\"continue\",\"command_id\":\"cmd-1\"} trailing\n-->")
	if parsed.Status != ValidationMalformed {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerMultipleMarkers(t *testing.T) {
	marker := "<!-- cddm-dashboard:result\n{\"version\":1,\"role\":\"implementor\",\"result\":\"continue\",\"command_id\":\"cmd-1\"}\n-->"
	parsed := ParseMarker(marker + "\n" + marker)
	if parsed.Status != ValidationMalformed || parsed.Reason != "multiple_markers" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerUnsupportedVersion(t *testing.T) {
	parsed := parsePayload(t, `{"version":3,"role":"implementor","result":"continue","command_id":"cmd-1"}`)
	if parsed.Status != ValidationUnsupported || parsed.Reason != "unsupported_version" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerMissingCommandID(t *testing.T) {
	parsed := parsePayload(t, `{"version":1,"role":"implementor","result":"continue","command_id":""}`)
	if parsed.Status != ValidationMalformed || parsed.Reason != "invalid_command_id" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerIgnoresExamples(t *testing.T) {
	body := "```html\n<!-- cddm-dashboard:result\n{\"version\":1}\n-->\n```\n\n> <!-- cddm-dashboard:result {\"version\":1} -->"
	if parsed := ParseMarker(body); parsed.Present {
		t.Fatalf("example became live marker: %+v", parsed)
	}
}

func TestParseMarkerQAContractsAcrossVersions(t *testing.T) {
	for _, version := range []int{1, 2} {
		approved := parsePayload(t, markerForQA(version, "approved", 0, ""))
		if approved.Status != ValidationAccepted {
			t.Fatalf("version %d approved = %+v", version, approved)
		}
		invalid := parsePayload(t, markerForQA(version, "approved", 1, ""))
		if invalid.Status != ValidationMalformed {
			t.Fatalf("version %d approved with finding = %+v", version, invalid)
		}
	}
}

func markerForQA(version int, result string, findings int, extra string) string {
	return `{"version":` + integerString(version) + `,"role":"qa","result":"` + result + `","command_id":"cmd-qa","reviewed_head":"` + testHead + `","blocking_findings":` + integerString(findings) + extra + `}`
}

func integerString(value int) string {
	if value == 0 {
		return "0"
	}
	if value == 1 {
		return "1"
	}
	if value == 2 {
		return "2"
	}
	return "3"
}

func parsePayload(t *testing.T, payload string) ParsedMarker {
	t.Helper()
	return ParseMarker("## Result\n\n<!-- cddm-dashboard:result\n" + payload + "\n-->")
}
