package orchestration_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
)

func TestProvisioningLifecycleIsLeaseBoundRestartSafeAndAttachmentExact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provisioning.db")
	db, project, store, scheduler, snapshot, commandID := schedulerFixture(t, path, 2, 2, 2)
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		MaxActiveWorkUnits: 2, MaxParallelImplementors: 2, MaxParallelQA: 2,
		ChatGPTProjectURL: "https://chatgpt.com/g/g-project/example/project",
	}); err != nil {
		t.Fatal(err)
	}
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "provision-impl", orchestration.ActionDispatch, 101, "implementor", 50, "project:1:issue:101:implementor"),
	})
	decision := claim(t, scheduler, project.ID, "schedule-provision", snapshot)
	service := provisioningService(t, store)
	request, err := service.Enqueue(context.Background(), orchestration.EnqueueProvisioningInput{
		ProjectID: project.ID, LeaseID: decision.Lease.ID, LeaseOwner: "scheduler", LeaseToken: decision.Lease.LeaseToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Role != "implementor" || request.SessionPolicy != orchestration.SessionPolicyFreshPerIntent || request.ChatGPTProjectURL != "https://chatgpt.com/g/g-project/example/project" {
		t.Fatalf("provision request = %+v", request)
	}
	wantAttachments := []string{"02-implementor-trigger.md", "gpt-gh-connector-guidelines.md"}
	if !sameStrings(request.Attachments, wantAttachments) || request.Status != orchestration.ProvisionPending {
		t.Fatalf("attachments/status = %+v / %s", request.Attachments, request.Status)
	}
	again, err := service.Enqueue(context.Background(), orchestration.EnqueueProvisioningInput{
		ProjectID: project.ID, LeaseID: decision.Lease.ID, LeaseOwner: "scheduler", LeaseToken: decision.Lease.LeaseToken,
	})
	if err != nil || again.ID != request.ID {
		t.Fatalf("idempotent enqueue = %+v err=%v", again, err)
	}
	if _, err := service.Enqueue(context.Background(), orchestration.EnqueueProvisioningInput{
		ProjectID: project.ID, LeaseID: decision.Lease.ID, LeaseOwner: "scheduler", LeaseToken: "wrong",
	}); !errors.Is(err, orchestration.ErrConflict) {
		t.Fatalf("wrong lease token error = %v", err)
	}

	claimed, err := service.ClaimNext(context.Background(), orchestration.ProvisionClaimInput{ClaimID: "extension-claim", ClaimOwner: "extension", ClaimTTL: time.Minute})
	if err != nil || claimed == nil || claimed.ClaimToken == "" || claimed.Status != orchestration.ProvisionClaimed {
		t.Fatalf("claim = %+v err=%v", claimed, err)
	}
	target := browserbinding.TargetRef{Kind: browserbinding.TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/provisioned-chat"}
	surface, err := service.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: claimed.ClaimToken,
		Outcome: orchestration.ProvisionSurfaceReady, WorkerID: "managed-worker", TabID: 42, Target: &target,
	})
	if err != nil || surface.Status != orchestration.ProvisionSurfaceReady || surface.Target == nil {
		t.Fatalf("surface ready = %+v err=%v", surface, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := provisioningService(t, orchestration.NewStore(reopened))
	recovered, err := restarted.ClaimNext(context.Background(), orchestration.ProvisionClaimInput{ClaimID: "extension-claim", ClaimOwner: "extension", ClaimTTL: time.Minute})
	if err != nil || recovered == nil || recovered.Status != orchestration.ProvisionSurfaceReady || recovered.TabID != 42 {
		t.Fatalf("recovered surface = %+v err=%v", recovered, err)
	}
	if _, err := restarted.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: recovered.ClaimToken,
		Outcome: orchestration.ProvisionProvisioned, AttachmentEvidence: []string{"gpt-gh-connector-guidelines.md"},
	}); err == nil {
		t.Fatal("provisioned accepted incomplete attachment evidence")
	}
	completed, err := restarted.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: recovered.ClaimToken,
		Outcome: orchestration.ProvisionProvisioned, AttachmentEvidence: wantAttachments,
	})
	if err != nil || completed.Status != orchestration.ProvisionProvisioned || completed.CompletedAt == nil {
		t.Fatalf("provisioned = %+v err=%v", completed, err)
	}
	if _, err := restarted.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: recovered.ClaimToken,
		Outcome: orchestration.ProvisionProvisioned, AttachmentEvidence: wantAttachments,
	}); !errors.Is(err, orchestration.ErrConflict) {
		t.Fatalf("terminal replay error = %v", err)
	}
}

func TestProvisioningRespectsPauseAndUncertainIsTerminal(t *testing.T) {
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 2, 2, 2)
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{
		schedulerIntent(project.ID, commandID, "paused-provision", orchestration.ActionDispatch, 101, "implementor", 50, "project:1:issue:101:implementor"),
	})
	decision := claim(t, scheduler, project.ID, "schedule-paused", snapshot)
	service := provisioningService(t, store)
	if _, err := service.Enqueue(context.Background(), orchestration.EnqueueProvisioningInput{
		ProjectID: project.ID, LeaseID: decision.Lease.ID, LeaseOwner: "scheduler", LeaseToken: decision.Lease.LeaseToken,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStatePaused, ControlIssueNumber: 90,
		MaxActiveWorkUnits: 2, MaxParallelImplementors: 2, MaxParallelQA: 2,
	}); err != nil {
		t.Fatal(err)
	}
	paused, err := service.ClaimNext(context.Background(), orchestration.ProvisionClaimInput{ClaimID: "paused-claim", ClaimOwner: "extension", ClaimTTL: time.Minute})
	if err != nil || paused != nil {
		t.Fatalf("paused claim = %+v err=%v", paused, err)
	}
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		MaxActiveWorkUnits: 2, MaxParallelImplementors: 2, MaxParallelQA: 2,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimNext(context.Background(), orchestration.ProvisionClaimInput{ClaimID: "uncertain-claim", ClaimOwner: "extension", ClaimTTL: time.Minute})
	if err != nil || claimed == nil {
		t.Fatalf("enabled claim = %+v err=%v", claimed, err)
	}
	uncertain, err := service.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: claimed.ID, ClaimOwner: "extension", ClaimToken: claimed.ClaimToken,
		Outcome: orchestration.ProvisionUncertain, Reason: "conversation_url_unobserved",
	})
	if err != nil || uncertain.Status != orchestration.ProvisionUncertain {
		t.Fatalf("uncertain completion = %+v err=%v", uncertain, err)
	}
	next, err := service.ClaimNext(context.Background(), orchestration.ProvisionClaimInput{ClaimID: "after-uncertain", ClaimOwner: "extension", ClaimTTL: time.Minute})
	if err != nil || next != nil {
		t.Fatalf("uncertain request replayed: %+v err=%v", next, err)
	}
}

func provisioningService(t *testing.T, store *orchestration.Store) *orchestration.ProvisioningService {
	t.Helper()
	pack, err := resourcepack.Load(resourcepack.V2Profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := orchestration.NewProvisioningService(store, pack)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
