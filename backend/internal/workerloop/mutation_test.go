package workerloop

import (
	"context"
	"testing"
)

func TestAcceptedMarkerMutationBecomesAmbiguous(t *testing.T) {
	_, project, store, service := testService(t, ":memory:")
	command := createCommand(t, store, project.ID, "cmd-mutated", "implementor", "", CommandAwaitingResult)
	first := projectSnapshot(project, issueWithComments(140, comment(1001, continueMarker(command.ID))))
	if err := service.ObserveProjectSnapshot(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	changed := projectSnapshot(project, issueWithComments(140, comment(1001, candidateMarker(command.ID, 150, headA))))
	if err := service.ObserveProjectSnapshot(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAmbiguous)
	results := commandResults(t, store, project.ID, command.ID)
	if len(results) != 1 || results[0].ValidationStatus != ValidationAmbiguous || results[0].ValidationReason != "accepted_result_mutated" {
		t.Fatalf("results = %+v", results)
	}
}
