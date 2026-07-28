package workerloop

import "testing"

func TestParseMarkerCandidateReady(t *testing.T) {
	parsed := ParseMarker(`## Implementor Handoff

<!-- cddm-dashboard:result
{"version":1,"role":"implementor","result":"candidate_ready","command_id":"cmd-1","pr":150,"head":"241401d9f5c1fb2004eeb19ec612323f74b57199"}
-->`)
	if !parsed.Present || parsed.Status != ValidationAccepted {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed.Payload.PR != 150 || parsed.Payload.Head == "" || parsed.Hash == "" {
		t.Fatalf("payload = %+v", parsed.Payload)
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
	parsed := ParseMarker("<!-- cddm-dashboard:result\n{\"version\":2,\"role\":\"implementor\",\"result\":\"continue\",\"command_id\":\"cmd-1\"}\n-->")
	if parsed.Status != ValidationUnsupported {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseMarkerMissingCommandID(t *testing.T) {
	parsed := ParseMarker("<!-- cddm-dashboard:result\n{\"version\":1,\"role\":\"implementor\",\"result\":\"continue\",\"command_id\":\"\"}\n-->")
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

func TestParseQAContracts(t *testing.T) {
	zero := 0
	approved := MarkerPayload{Version: 1, Role: "qa", Result: "approved", CommandID: "cmd-qa", ReviewedHead: "241401d9f5c1fb2004eeb19ec612323f74b57199", BlockingFindings: &zero}
	if status, reason := validatePayload(approved); status != ValidationAccepted {
		t.Fatalf("approved = %s/%s", status, reason)
	}
	approved.BlockingFindings = intPointer(1)
	if status, _ := validatePayload(approved); status != ValidationMalformed {
		t.Fatalf("approved with finding = %s", status)
	}
}

func intPointer(value int) *int { return &value }
