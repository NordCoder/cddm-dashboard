package orchestration_test

import (
	"context"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
)

func TestSurfaceReadyAcceptsExactTabWithoutConversationTarget(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 2, 2, 2)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "surface-no-target", orchestration.ActionDispatch, 101, "implementor", 50, "project:1:issue:101:implementor"),
	})
	decision := claim(t, scheduler, project.ID, "schedule-surface", snapshot)
	service := provisioningService(t, store)
	request, err := service.Enqueue(context.Background(), orchestration.EnqueueProvisioningInput{
		ProjectID: project.ID, LeaseID: decision.Lease.ID, LeaseOwner: "scheduler", LeaseToken: decision.Lease.LeaseToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimNext(context.Background(), orchestration.ProvisionClaimInput{
		ClaimID: "surface-claim", ClaimOwner: "extension", ClaimTTL: time.Minute,
	})
	if err != nil || claimed == nil {
		t.Fatalf("claim = %+v err=%v", claimed, err)
	}
	surface, err := service.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: claimed.ClaimToken,
		Outcome: orchestration.ProvisionSurfaceReady, WorkerID: "managed-worker", TabID: 77,
	})
	if err != nil || surface.Status != orchestration.ProvisionSurfaceReady || surface.Target != nil {
		t.Fatalf("surface_ready = %+v err=%v", surface, err)
	}
	if _, err := service.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: claimed.ClaimToken,
		Outcome: orchestration.ProvisionProvisioned,
		AttachmentEvidence: []string{"02-implementor-trigger.md", "gpt-gh-connector-guidelines.md"},
	}); err == nil {
		t.Fatal("provisioned accepted without an exact conversation target")
	}
}
