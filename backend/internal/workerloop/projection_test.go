package workerloop

import (
	"context"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

type staticPlannerHealth struct{ value planning.Health }

func (s staticPlannerHealth) Health(context.Context) planning.Health { return s.value }

func TestExecutionProfileDefaultsAndRejectsAutoMerge(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	resources, err := resourcepack.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	service := NewProjectionService(db, supervisor.NewStore(db), store, browserbinding.New(db, time.Minute), resources, staticPlannerHealth{value: planning.Health{Status: "disabled"}})
	profile, err := service.Profile(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ResourceProfile != resourcepack.DefaultProfile || profile.DeliveryMode != DeliveryModeReviewed || profile.QASessionMode != QAModeManualFresh || profile.AutoMerge {
		t.Fatalf("profile = %+v", profile)
	}
	profile.DeliveryMode = DeliveryModeAuto
	updated, err := service.UpdateProfile(context.Background(), profile)
	if err != nil || updated.DeliveryMode != DeliveryModeAuto {
		t.Fatalf("updated = %+v err=%v", updated, err)
	}
	profile.AutoMerge = true
	if _, err := service.UpdateProfile(context.Background(), profile); err == nil {
		t.Fatal("auto_merge=true was accepted")
	}
}

func TestWorkUnitProjectionSeparatesDeliveryExecutionAndResult(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	resources, err := resourcepack.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	command := createTestWorkflowCommand(t, store, project.ID, "cmd-projection")
	if _, err := store.SetCommandStatus(context.Background(), command.ID, CommandAwaitingResult); err != nil {
		t.Fatal(err)
	}
	insertTestDeliveryCommand(t, db, project.ID, command.IssueNumber, "delivery-projection", "delivered")
	if _, err := db.Exec(`INSERT INTO workflow_delivery_links(workflow_command_id,delivery_command_id,created_at) VALUES(?,?,?)`, command.ID, "delivery-projection", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	accepted := time.Now().UTC()
	if err := store.UpsertResult(context.Background(), Result{
		ProjectID: project.ID, GitHubCommentID: 1001, IssueNumber: command.IssueNumber,
		CommandID: command.ID, Role: "implementor", Result: "continue", Payload: []byte(`{"version":1}`),
		PayloadHash: "hash", ValidationStatus: ValidationAccepted, AcceptedAt: &accepted, ObservedAt: accepted,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewProjectionService(db, supervisor.NewStore(db), store, browserbinding.New(db, time.Minute), resources, staticPlannerHealth{value: planning.Health{Status: "disabled"}})
	value, err := service.WorkUnit(context.Background(), project.ID, command.IssueNumber)
	if err != nil {
		t.Fatal(err)
	}
	if value.DeliveryStatus != "delivered" || value.ExecutionStatus != CommandAwaitingResult || value.ValidationStatus != ValidationAccepted {
		t.Fatalf("projection = %+v", value)
	}
	if value.WorkerResult == nil || value.WorkerResult.GitHubCommentID != 1001 {
		t.Fatalf("worker result = %+v", value.WorkerResult)
	}
}

func TestPilotReadinessUsesRoleBindingsWithoutRequiringQABinding(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	projectStore := supervisor.NewStore(db)
	if err := projectStore.ReplaceSnapshot(context.Background(), project.ID, supervisor.RepositorySnapshot{
		FetchedAt: time.Now().UTC(),
		Issues: []supervisor.Issue{{GitHubID: 1400, Number: 140, Title: "Pilot", State: "open", URL: "https://github.com/NordCoder/misak-website/issues/140"}},
	}); err != nil {
		t.Fatal(err)
	}
	resources, err := resourcepack.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	bindings := browserbinding.New(db, time.Minute)
	for _, role := range []string{"lead", "implementor"} {
		worker := "worker-" + role
		target := browserbinding.TargetRef{Kind: browserbinding.TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/" + role}
		if _, err := bindings.Register(context.Background(), browserbinding.RegisterInput{WorkerID: worker, SessionID: "session-" + role, Observation: browserbinding.Observation{Target: &target}}); err != nil {
			t.Fatal(err)
		}
		if _, err := bindings.Put(context.Background(), project.ID, LogicalLane(project, 140, role), browserbinding.PutInput{WorkerID: worker, Target: target}); err != nil {
			t.Fatal(err)
		}
	}
	service := NewProjectionService(db, projectStore, store, bindings, resources, staticPlannerHealth{value: planning.Health{Status: "disabled"}})
	readiness, err := service.Readiness(context.Background(), project.ID, 140)
	if err != nil {
		t.Fatal(err)
	}
	if !readiness.Ready || readiness.Status != "pilot_ready" {
		t.Fatalf("readiness = %+v", readiness)
	}
}
