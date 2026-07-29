package orchestration_test

import (
	"context"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func TestTypedMergeCommandCannotMaterializeActionsReadyBatch(t *testing.T) {
	fixture := newMergeResultFixture(t)
	allowed, err := fixture.engine.AllowActionMaterialization(context.Background(), fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("typed merge command was allowed to materialize an actions_ready batch")
	}

	allowed, err = fixture.engine.AllowActionMaterialization(context.Background(), workerloop.Command{ID: "unowned-command"})
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("unowned command was blocked by merge-specific action gate")
	}
}

func TestMergeActionsReadyFailsBeforePartialIntentMaterialization(t *testing.T) {
	fixture := newMergeResultFixture(t)
	acceptedAt := time.Now().UTC()
	result := workerloop.Result{
		ProjectID: fixture.project.ID, GitHubCommentID: 3201, IssueNumber: 101,
		CommandID: fixture.command.ID, Role: "lead", Result: "actions_ready",
		PayloadHash: "forbidden-actions-hash", ValidationStatus: workerloop.ValidationAccepted,
		AcceptedAt: &acceptedAt, ObservedAt: acceptedAt,
	}
	payload := workerloop.MarkerPayload{
		Version: 2, Role: "lead", Result: "actions_ready", CommandID: fixture.command.ID,
		Actions: []workerloop.ActionPayload{{
			ActionID: "forbidden-dispatch", Type: "dispatch", Repository: "NordCoder/app",
			Issue: 102, Role: "implementor",
		}},
		Wave: &workerloop.WavePayload{WaveID: "forbidden-wave", ControlIssue: 90, Issues: []int{102}},
	}
	pipeline := orchestration.NewResultMaterializationPipeline(
		orchestration.NewCommandResultMux(fixture.engine), orchestration.NewMaterializer(fixture.store),
	)
	snapshot := supervisor.ProjectSnapshot{Project: fixture.project, Issues: []supervisor.Issue{
		{Number: 90, State: "open"}, {Number: 101, State: "open"}, {Number: 102, State: "open"},
	}}
	if err := pipeline.ReconcileResult(context.Background(), snapshot, fixture.command, result, payload); err != nil {
		t.Fatal(err)
	}
	intents, err := fixture.store.ListIntents(context.Background(), fixture.project.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range intents {
		if intent.ActionID == "forbidden-dispatch" || intent.WaveID == "forbidden-wave" {
			t.Fatalf("partial action Intent materialized: %+v", intent)
		}
	}
	if _, err := fixture.store.Wave(context.Background(), fixture.project.ID, "forbidden-wave"); err == nil {
		t.Fatal("forbidden Wave was materialized")
	}
	assertMergeResultState(t, fixture, orchestration.IntentAmbiguous, orchestration.LeaseSuperseded, orchestration.AutonomousMaterializationAmbiguous, orchestration.MergeCycleAmbiguous)
}
