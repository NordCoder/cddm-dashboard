package workerloop

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
)

func TestWorkflowDeliveryLinkIsImmutable(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	command := createTestWorkflowCommand(t, store, project.ID, "cmd-link")
	insertTestDeliveryCommand(t, db, project.ID, command.IssueNumber, "delivery-a", delivery.StatusPending)
	insertTestDeliveryCommand(t, db, project.ID, command.IssueNumber, "delivery-b", delivery.StatusPending)

	coordinator := NewDeliveryCoordinator(db, staticBrowserDelivery{}, nil, nil)
	if err := coordinator.link(context.Background(), command.ID, "delivery-a"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.link(context.Background(), command.ID, "delivery-a"); err != nil {
		t.Fatalf("same pair is not idempotent: %v", err)
	}
	if err := coordinator.link(context.Background(), command.ID, "delivery-b"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second delivery link error = %v, want conflict", err)
	}
	linked, err := coordinator.linkedDelivery(context.Background(), command.ID)
	if err != nil {
		t.Fatal(err)
	}
	if linked != "delivery-a" {
		t.Fatalf("linked delivery = %q, want delivery-a", linked)
	}
}

func TestDeliveryCompletionRefreshesWorkflowProjection(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	command := createTestWorkflowCommand(t, store, project.ID, "cmd-complete")
	insertTestDeliveryCommand(t, db, project.ID, command.IssueNumber, "delivery-complete", delivery.StatusPending)

	refresher := &recordingProjectRefresher{}
	base := staticBrowserDelivery{completed: delivery.Command{
		ID:        "delivery-complete",
		ProjectID: project.ID,
		Status:    delivery.StatusDelivered,
	}}
	coordinator := NewDeliveryCoordinator(db, base, nil, NewCommandEngine(store, resourcepack.Package{}), refresher)
	if err := coordinator.link(context.Background(), command.ID, "delivery-complete"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Complete(context.Background(), delivery.Completion{}); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAwaitingResult)
	if len(refresher.projects) != 1 || refresher.projects[0] != project.ID {
		t.Fatalf("refreshed projects = %v, want [%d]", refresher.projects, project.ID)
	}
}

func TestRecoveryReconciliationRefreshesAffectedProjects(t *testing.T) {
	db, project, store, _ := testService(t, ":memory:")
	command := createTestWorkflowCommand(t, store, project.ID, "cmd-reconcile")
	insertTestDeliveryCommand(t, db, project.ID, command.IssueNumber, "delivery-reconcile", delivery.StatusDelivered)

	refresher := &recordingProjectRefresher{}
	coordinator := NewDeliveryCoordinator(db, staticBrowserDelivery{}, nil, NewCommandEngine(store, resourcepack.Package{}), refresher)
	if err := coordinator.link(context.Background(), command.ID, "delivery-reconcile"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCommandStatus(t, store, command.ID, CommandAwaitingResult)
	if len(refresher.projects) != 1 || refresher.projects[0] != project.ID {
		t.Fatalf("refreshed projects = %v, want [%d]", refresher.projects, project.ID)
	}
}

func createTestWorkflowCommand(t *testing.T, store *Store, projectID int64, id string) Command {
	t.Helper()
	command, err := store.CreateCommand(context.Background(), CreateCommandInput{
		ID: id, ProjectID: projectID, IssueNumber: 140, IdentityKey: id + "-identity",
		Role: "implementor", Action: "dispatch", ResourceProfile: "cddm-dashboard-resources/v1.0",
		ContextHash: "context", Status: CommandDeliveryPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func insertTestDeliveryCommand(t *testing.T, db *sql.DB, projectID int64, issueNumber int, id, status string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO delivery_commands (
		id,project_id,issue_number,plan_id,idempotency_key,confirmation_fingerprint,
		plan_hash,context_hash,prompt_hash,prompt_text,action,target_role,lane_key,
		expected_head,binding_id,binding_version,worker_id,worker_session_id,
		presence_token,target_kind,target_ref,status,created_at,expires_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, projectID, issueNumber, 1, id+"-key", id+"-fingerprint",
		"plan", "context", "prompt", "prompt", "dispatch", "implementor", "lane",
		"", "binding", 1, "worker", "session", "presence", "chatgpt", "conversation",
		status, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
}

type recordingProjectRefresher struct {
	projects []int64
}

func (r *recordingProjectRefresher) RefreshProject(_ context.Context, projectID int64) error {
	r.projects = append(r.projects, projectID)
	return nil
}

type staticBrowserDelivery struct {
	completed delivery.Command
}

func (s staticBrowserDelivery) Create(context.Context, int64, int, delivery.Confirmation) (delivery.Command, error) {
	return delivery.Command{}, nil
}

func (s staticBrowserDelivery) List(context.Context, int64, int) ([]delivery.Command, error) {
	return nil, nil
}

func (s staticBrowserDelivery) ClaimNext(context.Context, delivery.ClaimRequest) (*delivery.Execution, error) {
	return nil, nil
}

func (s staticBrowserDelivery) Complete(context.Context, delivery.Completion) (delivery.Command, error) {
	return s.completed, nil
}

func (s staticBrowserDelivery) Reconcile(context.Context) error {
	return nil
}
