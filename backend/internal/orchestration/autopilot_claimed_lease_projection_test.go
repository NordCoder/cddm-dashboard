package orchestration_test

import (
	"context"
	"testing"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
)

func TestOperationsProjectionRejectsClaimedIntentWithoutActiveLease(t *testing.T) {
	ctx := context.Background()
	db, project, store, _, _, sourceCommandID := schedulerFixture(t, ":memory:", 1, 1, 1)
	input := schedulerIntent(project.ID, sourceCommandID, "claimed-without-lease", orchestration.ActionDispatch, 101, "implementor", 10, "project:1:issue:101:implementor")
	createSchedulerIntents(t, store, project.ID, sourceCommandID, []orchestration.IntentInput{input})
	if _, err := db.ExecContext(ctx, `UPDATE workflow_intents SET status='claimed' WHERE project_id=? AND id=?`, project.ID, input.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestration.NewOperationsService(store).Status(ctx, project.ID); err == nil {
		t.Fatal("claimed Intent without an active lease was projected as healthy")
	}
}
