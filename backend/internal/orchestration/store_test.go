package orchestration_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

const testHead = "241401d9f5c1fb2004eeb19ec612323f74b57199"

func TestProjectProfilePreservesV1DefaultsAndAcceptsExactV2(t *testing.T) {
	db, project := testProject(t, ":memory:")
	store := orchestration.NewStore(db)
	profile, err := store.ProjectProfile(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.AutonomyMode != orchestration.AutonomyModeManual || profile.AutonomyState != orchestration.AutonomyStateDisabled || profile.ResourceProfile != orchestration.ManualResourceProfile || profile.ControlIssueNumber != 0 {
		t.Fatalf("default profile = %+v", profile)
	}

	updated, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		DeliveryMode: "auto", MaxActiveWorkUnits: 4, MaxParallelImplementors: 3, MaxParallelQA: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ResourceProfile != orchestration.ContinuousResourceProfile || updated.Methodology != orchestration.ContinuousMethodology || updated.ResultProtocol != orchestration.ContinuousResultProtocol || updated.AutoMerge {
		t.Fatalf("continuous profile = %+v", updated)
	}

	_, err = store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		ResourceProfile: orchestration.ManualResourceProfile,
	})
	if err == nil {
		t.Fatal("continuous mode accepted a v1 resource profile")
	}
	_, err = store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeManual,
		AutonomyState: orchestration.AutonomyStateEnabled,
	})
	if err == nil {
		t.Fatal("manual mode accepted an enabled autonomy state")
	}
}

func TestIntentWaveBatchIsAtomicOrderedAndIdempotent(t *testing.T) {
	db, project := testProject(t, ":memory:")
	command, resultID := seedLeadResult(t, db, project.ID)
	store := orchestration.NewStore(db)
	wave := &orchestration.WaveInput{
		ProjectID: project.ID, WaveID: "wave-1", ControlIssueNumber: 90,
		SourceCommandID: command.ID, Status: orchestration.WavePlanned, Issues: []int{101, 102},
	}
	inputs := []orchestration.IntentInput{
		{ID: "intent-1", ProjectID: project.ID, SourceResultCommentID: resultID, SourceCommandID: command.ID, ActionID: "a-1", ActionType: orchestration.ActionDispatch, Repository: "NordCoder/app", IssueNumber: 101, Role: "implementor", WaveID: "wave-1", Priority: 50, Status: orchestration.IntentPending},
		{ID: "intent-2", ProjectID: project.ID, SourceResultCommentID: resultID, SourceCommandID: command.ID, ActionID: "a-2", ActionType: orchestration.ActionDispatch, Repository: "NordCoder/app", IssueNumber: 102, Role: "qa", ExpectedHead: testHead, WaveID: "wave-1", Priority: 40, Status: orchestration.IntentPending},
	}
	created, err := store.CreateBatch(context.Background(), wave, inputs)
	if err != nil || len(created) != 2 {
		t.Fatalf("CreateBatch = %+v err=%v", created, err)
	}
	again, err := store.CreateBatch(context.Background(), wave, inputs)
	if err != nil || len(again) != 2 || again[0].ID != created[0].ID {
		t.Fatalf("idempotent CreateBatch = %+v err=%v", again, err)
	}
	storedWave, err := store.Wave(context.Background(), project.ID, "wave-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(storedWave.Issues) != 2 || storedWave.Issues[0] != 101 || storedWave.Issues[1] != 102 {
		t.Fatalf("Wave membership = %+v", storedWave.Issues)
	}
	listed, err := store.ListIntents(context.Background(), project.ID, orchestration.IntentPending)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != "intent-2" || listed[1].ID != "intent-1" {
		t.Fatalf("priority ordering = %+v", listed)
	}

	conflicting := append([]orchestration.IntentInput(nil), inputs...)
	conflicting[0].Repository = "Other/app"
	if _, err := store.CreateBatch(context.Background(), wave, conflicting); !errors.Is(err, orchestration.ErrConflict) {
		t.Fatalf("conflicting batch error = %v", err)
	}
	listed, err = store.ListIntents(context.Background(), project.ID, "")
	if err != nil || len(listed) != 2 {
		t.Fatalf("partial conflict changed intents: count=%d err=%v", len(listed), err)
	}
}

func TestIntentWaveStorageSurvivesRestartAndCascades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orchestration.db")
	db, project := testProject(t, path)
	command, resultID := seedLeadResult(t, db, project.ID)
	store := orchestration.NewStore(db)
	_, err := store.CreateBatch(context.Background(), &orchestration.WaveInput{
		ProjectID: project.ID, WaveID: "wave-restart", ControlIssueNumber: 90,
		SourceCommandID: command.ID, Status: orchestration.WaveActive, Issues: []int{101},
	}, []orchestration.IntentInput{{
		ID: "intent-restart", ProjectID: project.ID, SourceResultCommentID: resultID,
		SourceCommandID: command.ID, ActionID: "a-restart", ActionType: orchestration.ActionCorrect,
		Repository: "NordCoder/app", IssueNumber: 101, Role: "implementor",
		ExpectedPreviousHead: testHead, WaveID: "wave-restart", Priority: 10, Status: orchestration.IntentPending,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := orchestration.NewStore(reopened)
	if _, err := restarted.Intent(context.Background(), "intent-restart"); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.NewStore(reopened).DeleteProject(context.Background(), project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Intent(context.Background(), "intent-restart"); !errors.Is(err, orchestration.ErrNotFound) {
		t.Fatalf("intent survived Project deletion: %v", err)
	}
	if _, err := restarted.Wave(context.Background(), project.ID, "wave-restart"); !errors.Is(err, orchestration.ErrNotFound) {
		t.Fatalf("Wave survived Project deletion: %v", err)
	}
}

func testProject(t *testing.T, path string) (*sql.DB, supervisor.Project) {
	t.Helper()
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if path == ":memory:" {
		t.Cleanup(func() { db.Close() })
	}
	project, err := supervisor.NewStore(db).CreateProject(context.Background(), supervisor.CreateProjectInput{
		Owner: "NordCoder", Repository: "app", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, project
}

func seedLeadResult(t *testing.T, db *sql.DB, projectID int64) (workerloop.Command, int64) {
	t.Helper()
	commands := workerloop.NewStore(db)
	command, err := commands.CreateCommand(context.Background(), workerloop.CreateCommandInput{
		ID: "cmd-lead", ProjectID: projectID, IssueNumber: 90, IdentityKey: "cmd-lead-identity",
		Role: "lead", Action: "dispatch", ResourceProfile: orchestration.ContinuousResourceProfile,
		ContextHash: "context", Status: workerloop.CommandCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	const resultID int64 = 2001
	if err := commands.UpsertResult(context.Background(), workerloop.Result{
		ProjectID: projectID, GitHubCommentID: resultID, IssueNumber: 90, CommandID: command.ID,
		Role: "lead", Result: "actions_ready", Payload: []byte(`{"version":2}`), PayloadHash: "hash",
		ValidationStatus: workerloop.ValidationAccepted, AcceptedAt: &now, ObservedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return command, resultID
}
