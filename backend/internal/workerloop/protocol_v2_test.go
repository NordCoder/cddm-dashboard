package workerloop

import (
	"context"
	"fmt"
	"testing"
)

func TestV2ActionsReadyCompletesLeadCommandAsEvidenceOnly(t *testing.T) {
	_, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-v2-actions", "lead", "", CommandAwaitingResult)
	body := marker(fmt.Sprintf(`{
  "version":2,
  "role":"lead",
  "result":"actions_ready",
  "command_id":%q,
  "actions":[
    {"action_id":"a-1","type":"dispatch","repository":"NordCoder/misak-website","issue":141,"role":"implementor"}
  ],
  "wave":{"wave_id":"wave-1","control_issue":140,"issues":[141]}
}`, command.ID))

	if err := service.ObserveProjectSnapshot(context.Background(), projectSnapshot(project, issueWithComments(140, comment(2001, body)))); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandCompleted)
	results := commandResults(t, store, project.ID, command.ID)
	if len(results) != 1 || results[0].ValidationStatus != ValidationAccepted || results[0].Result != "actions_ready" {
		t.Fatalf("results = %+v", results)
	}
}

func TestV2MergedRequiresCommandExpectedHead(t *testing.T) {
	_, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-v2-merge", "lead", headA, CommandAwaitingResult)
	stale := marker(fmt.Sprintf(`{
  "version":2,"role":"lead","result":"merged","command_id":%q,
  "repository":"NordCoder/misak-website","issue":140,"pr":150,
  "approved_head":%q,"merge_commit":%q
}`, command.ID, headB, headA))

	if err := service.ObserveProjectSnapshot(context.Background(), projectSnapshot(project, issueWithComments(140, comment(2002, stale)))); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAwaitingResult)
	results := commandResults(t, store, project.ID, command.ID)
	if len(results) != 1 || results[0].ValidationStatus != ValidationStale {
		t.Fatalf("stale results = %+v", results)
	}
}

func TestV2QAInconclusiveMapsToInconclusiveCommand(t *testing.T) {
	_, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-v2-qa", "qa", headA, CommandAwaitingResult)
	body := marker(fmt.Sprintf(`{
  "version":2,"role":"qa","result":"inconclusive","command_id":%q,
  "reviewed_head":%q,"blocking_findings":0,
  "blocker_type":"infrastructure","reason_code":"ci_unavailable"
}`, command.ID, headA))

	if err := service.ObserveProjectSnapshot(context.Background(), projectSnapshot(project, issueWithComments(140, comment(2003, body)))); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandInconclusive)
}
