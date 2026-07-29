package orchestration_test

import (
	"context"
	"testing"

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
