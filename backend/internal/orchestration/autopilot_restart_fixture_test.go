package orchestration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func persistRestartSoakSnapshot(t *testing.T, db *sql.DB, project supervisor.Project) supervisor.ProjectSnapshot {
	t.Helper()
	now := time.Now().UTC()
	ci := func(head string) supervisor.CISummary {
		return supervisor.CISummary{HeadSHA: head, Status: "completed", Conclusion: "success", Source: "restart-fixture", UpdatedAt: now}
	}
	pull := func(id int64, number int, head string) supervisor.PullRequest {
		return supervisor.PullRequest{
			GitHubID: id, Number: number, Title: "Restart Candidate", State: "open", Draft: true,
			MergeableState: "clean", BaseRef: "change/base", HeadRef: fmt.Sprintf("restart/%d", number), HeadSHA: head,
			URL: fmt.Sprintf("https://github.com/NordCoder/app/pull/%d", number), UpdatedAt: now, CI: ci(head),
		}
	}
	issues := []supervisor.Issue{
		{GitHubID: 9001, Number: 90, Title: "Control", State: "open", CreatedAt: now, UpdatedAt: now},
		{GitHubID: 9101, Number: 101, Title: "Pending", State: "open", CreatedAt: now, UpdatedAt: now},
		{GitHubID: 9102, Number: 102, Title: "Claimed", State: "open", CreatedAt: now, UpdatedAt: now},
		{GitHubID: 9104, Number: 104, Title: "Delivery pending", State: "open", CreatedAt: now, UpdatedAt: now, PullRequests: []supervisor.PullRequest{pull(9154, 154, otherHead)}},
		{GitHubID: 9105, Number: 105, Title: "Awaiting QA result", State: "open", CreatedAt: now, UpdatedAt: now, PullRequests: []supervisor.PullRequest{pull(9155, 155, testHead)}},
	}
	if err := supervisor.NewStore(db).ReplaceSnapshot(context.Background(), project.ID, supervisor.RepositorySnapshot{FetchedAt: now, Issues: issues}); err != nil {
		t.Fatal(err)
	}
	return supervisor.ProjectSnapshot{Project: project, Issues: issues}
}

func claimSoakProvision(t *testing.T, service *orchestration.ProvisioningService, projectID int64, lease orchestration.Lease, stage string) (orchestration.ProvisionRequest, orchestration.ProvisionRequest) {
	t.Helper()
	request, err := service.Enqueue(context.Background(), orchestration.EnqueueProvisioningInput{
		ProjectID: projectID, LeaseID: lease.ID, LeaseOwner: lease.LeaseOwner, LeaseToken: lease.LeaseToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimNext(context.Background(), orchestration.ProvisionClaimInput{
		ClaimID: "provision-" + stage, ClaimOwner: "extension-" + stage, ClaimTTL: time.Minute,
	})
	if err != nil || claimed == nil || claimed.ID != request.ID {
		t.Fatalf("claim provisioning %s = %+v err=%v", stage, claimed, err)
	}
	return request, *claimed
}

func finalizeSoakProvision(t *testing.T, service *orchestration.ProvisioningService, finalizer *orchestration.ProvisioningFinalizer, bindings *browserbinding.Service, projectID int64, lease orchestration.Lease, stage string, tabID int) orchestration.ProvisionRequest {
	t.Helper()
	request, claimed := claimSoakProvision(t, service, projectID, lease, stage)
	workerID := "worker-" + stage
	sessionID := "session-" + stage
	target := browserbinding.TargetRef{Kind: browserbinding.TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/restart-" + stage}
	if _, err := service.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: claimed.ClaimOwner, ClaimToken: claimed.ClaimToken,
		Outcome: orchestration.ProvisionSurfaceReady, WorkerID: workerID, TabID: tabID,
	}); err != nil {
		t.Fatal(err)
	}
	worker, err := bindings.Register(context.Background(), browserbinding.RegisterInput{
		WorkerID: workerID, SessionID: sessionID, ProtocolVersion: "m12-c4",
		Capabilities: []string{"managed_exact_tab", "library_bootstrap"}, Observation: browserbinding.Observation{Target: &target},
	})
	if err != nil || worker.State != "live" || worker.SessionID != sessionID {
		t.Fatalf("register %s worker = %+v err=%v", stage, worker, err)
	}
	finalized, err := finalizer.Finalize(context.Background(), orchestration.FinalizeProvisioningInput{
		RequestID: request.ID, ClaimOwner: claimed.ClaimOwner, ClaimToken: claimed.ClaimToken,
		WorkerID: workerID, TabID: tabID, Target: target,
		ObservedChatGPTURL: "https://chatgpt.com/g/g-project/repository" + target.Path,
		AttachmentEvidence: append([]string(nil), request.Attachments...),
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != orchestration.ProvisionProvisioned || finalized.BoundBindingID == "" || finalized.BoundBindingVersion <= 0 || finalized.Target == nil {
		t.Fatalf("finalized %s provisioning = %+v", stage, finalized)
	}
	return finalized
}
