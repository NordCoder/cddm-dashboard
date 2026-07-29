package orchestration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

const otherHead = "341401d9f5c1fb2004eeb19ec612323f74b57199"

func TestAcceptedV2ActionsMaterializeWaveAndEveryActionType(t *testing.T) {
	db, project := testProject(t, ":memory:")
	orchestrationStore, service, command := materializationService(t, db, project, orchestration.AutonomyStateEnabled)
	body := actionsMarker(command.ID, "NordCoder/app", "a-1")
	snapshot := materializationSnapshot(project, body)

	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	intents, err := orchestrationStore.ListIntents(context.Background(), project.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 7 {
		t.Fatalf("intent count = %d, want 7: %+v", len(intents), intents)
	}
	statuses := make(map[string]string)
	for _, intent := range intents {
		statuses[intent.ActionType+":"+intent.ActionID] = intent.Status
		if intent.ActionType != orchestration.ActionHold && intent.ActionType != orchestration.ActionOwnerRequired && intent.LaneKey == "" {
			t.Fatalf("runnable intent has no lane: %+v", intent)
		}
	}
	if statuses[orchestration.ActionHold+":a-6"] != orchestration.IntentBlocked || statuses[orchestration.ActionOwnerRequired+":a-7"] != orchestration.IntentBlocked {
		t.Fatalf("blocking action statuses = %+v", statuses)
	}
	wave, err := orchestrationStore.Wave(context.Background(), project.ID, "wave-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(wave.Issues) != 2 || wave.Issues[0] != 101 || wave.Issues[1] != 102 {
		t.Fatalf("Wave = %+v", wave)
	}
	outcome, err := orchestrationStore.Materialization(context.Background(), project.ID, 3001)
	if err != nil || outcome.Status != orchestration.MaterializationMaterialized {
		t.Fatalf("outcome = %+v err=%v", outcome, err)
	}

	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	again, err := orchestrationStore.ListIntents(context.Background(), project.ID, "")
	if err != nil || len(again) != len(intents) {
		t.Fatalf("duplicate sync intents = %d err=%v", len(again), err)
	}
}

func TestPausedEvidenceCanMaterializeAfterEnable(t *testing.T) {
	db, project := testProject(t, ":memory:")
	store, service, command := materializationService(t, db, project, orchestration.AutonomyStatePaused)
	snapshot := materializationSnapshot(project, actionsMarker(command.ID, "NordCoder/app", "a-paused"))
	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	outcome, err := store.Materialization(context.Background(), project.ID, 3001)
	if err != nil || outcome.Status != orchestration.MaterializationSkipped || outcome.ReasonCode != "autonomy_paused" {
		t.Fatalf("paused outcome = %+v err=%v", outcome, err)
	}
	if intents, err := store.ListIntents(context.Background(), project.ID, ""); err != nil || len(intents) != 0 {
		t.Fatalf("paused intents = %+v err=%v", intents, err)
	}
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	outcome, err = store.Materialization(context.Background(), project.ID, 3001)
	if err != nil || outcome.Status != orchestration.MaterializationMaterialized {
		t.Fatalf("enabled outcome = %+v err=%v", outcome, err)
	}
}

func TestTargetMismatchBlocksWithoutPartialIntents(t *testing.T) {
	db, project := testProject(t, ":memory:")
	store, service, command := materializationService(t, db, project, orchestration.AutonomyStateEnabled)
	snapshot := materializationSnapshot(project, actionsMarker(command.ID, "Other/app", "a-mismatch"))
	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	outcome, err := store.Materialization(context.Background(), project.ID, 3001)
	if err != nil || outcome.Status != orchestration.MaterializationBlocked || outcome.ReasonCode != "repository_mismatch" {
		t.Fatalf("blocked outcome = %+v err=%v", outcome, err)
	}
	if intents, err := store.ListIntents(context.Background(), project.ID, ""); err != nil || len(intents) != 0 {
		t.Fatalf("mismatched batch created intents = %+v err=%v", intents, err)
	}
}

func TestAcceptedResultMutationMakesMaterializationAndIntentsAmbiguous(t *testing.T) {
	db, project := testProject(t, ":memory:")
	store, service, command := materializationService(t, db, project, orchestration.AutonomyStateEnabled)
	first := materializationSnapshot(project, actionsMarker(command.ID, "NordCoder/app", "a-original"))
	if err := service.ObserveProjectSnapshot(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	mutated := materializationSnapshot(project, actionsMarker(command.ID, "NordCoder/app", "a-mutated"))
	if err := service.ObserveProjectSnapshot(context.Background(), mutated); err != nil {
		t.Fatal(err)
	}
	outcome, err := store.Materialization(context.Background(), project.ID, 3001)
	if err != nil || outcome.Status != orchestration.MaterializationAmbiguous {
		t.Fatalf("ambiguous outcome = %+v err=%v", outcome, err)
	}
	intents, err := store.ListIntents(context.Background(), project.ID, "")
	if err != nil || len(intents) == 0 {
		t.Fatalf("ambiguous intents = %+v err=%v", intents, err)
	}
	for _, intent := range intents {
		if intent.Status != orchestration.IntentAmbiguous {
			t.Fatalf("intent not ambiguous: %+v", intent)
		}
	}
}

func TestV1LeadResultRemainsEvidenceOnly(t *testing.T) {
	db, project := testProject(t, ":memory:")
	store := orchestration.NewStore(db)
	commands := workerloop.NewStore(db)
	command, err := commands.CreateCommand(context.Background(), workerloop.CreateCommandInput{
		ID: "cmd-v1-lead", ProjectID: project.ID, IssueNumber: 90, IdentityKey: "v1-lead",
		Role: "lead", Action: "dispatch", ResourceProfile: orchestration.ManualResourceProfile,
		ContextHash: "context", Status: workerloop.CommandAwaitingResult,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := workerloop.NewService(commands)
	service.SetResultMaterializer(orchestration.NewMaterializer(store))
	now := time.Now().UTC()
	snapshot := supervisor.ProjectSnapshot{Project: project, Issues: []supervisor.Issue{{
		Number: 90, Comments: []supervisor.Comment{{GitHubID: 3001, Body: fmt.Sprintf("<!-- cddm-dashboard:result\n{\"version\":1,\"role\":\"lead\",\"result\":\"continue\",\"command_id\":%q}\n-->", command.ID), CreatedAt: now, UpdatedAt: now}},
	}}}
	if err := service.ObserveProjectSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Materialization(context.Background(), project.ID, 3001); err != orchestration.ErrNotFound {
		t.Fatalf("v1 result produced materialization: %v", err)
	}
}

func materializationService(t *testing.T, db *sql.DB, project supervisor.Project, state string) (*orchestration.Store, *workerloop.Service, workerloop.Command) {
	t.Helper()
	store := orchestration.NewStore(db)
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: state, ControlIssueNumber: 90, DeliveryMode: "auto",
	}); err != nil {
		t.Fatal(err)
	}
	commands := workerloop.NewStore(db)
	command, err := commands.CreateCommand(context.Background(), workerloop.CreateCommandInput{
		ID: "cmd-materialize", ProjectID: project.ID, IssueNumber: 90, IdentityKey: "materialize-identity",
		Role: "lead", Action: "dispatch", ResourceProfile: orchestration.ContinuousResourceProfile,
		ContextHash: "context", Status: workerloop.CommandAwaitingResult,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := workerloop.NewService(commands)
	service.SetResultMaterializer(orchestration.NewMaterializer(store))
	return store, service, command
}

func materializationSnapshot(project supervisor.Project, body string) supervisor.ProjectSnapshot {
	now := time.Now().UTC()
	return supervisor.ProjectSnapshot{Project: project, Issues: []supervisor.Issue{
		{Number: 90, State: "open", Comments: []supervisor.Comment{{GitHubID: 3001, Body: body, CreatedAt: now, UpdatedAt: now}}},
		{Number: 101, State: "open", PullRequests: []supervisor.PullRequest{{Number: 150, HeadSHA: testHead, State: "open"}}},
		{Number: 102, State: "open", PullRequests: []supervisor.PullRequest{{Number: 151, HeadSHA: otherHead, State: "open"}}},
	}}
}

func actionsMarker(commandID, repository, firstActionID string) string {
	return fmt.Sprintf(`<!-- cddm-dashboard:result
{
  "version":2,
  "role":"lead",
  "result":"actions_ready",
  "command_id":%q,
  "actions":[
    {"action_id":%q,"type":"dispatch","repository":%q,"issue":101,"role":"implementor"},
    {"action_id":"a-2","type":"dispatch","repository":%q,"issue":102,"role":"qa","expected_head":%q},
    {"action_id":"a-3","type":"correct","repository":%q,"issue":101,"role":"implementor","expected_previous_head":%q},
    {"action_id":"a-4","type":"merge_candidate","repository":%q,"issue":101,"role":"lead","pr":150,"expected_head":%q},
    {"action_id":"a-5","type":"plan_next_wave","repository":%q,"issue":90,"role":"lead"},
    {"action_id":"a-6","type":"hold","repository":%q,"issue":101,"reason_code":"dependency_wait"},
    {"action_id":"a-7","type":"owner_required","repository":%q,"issue":0,"reason_code":"scope_choice","decision_category":"scope"}
  ],
  "wave":{"wave_id":"wave-1","control_issue":90,"issues":[101,102]}
}
-->`, commandID, firstActionID, repository, repository, otherHead, repository, testHead, repository, testHead, repository, repository, repository)
}
