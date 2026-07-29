package orchestration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

const finalProjectURL = "https://chatgpt.com/g/g-project/repository/project"

func TestFinalizeProvisioningAtomicallyBindsExactLiveSession(t *testing.T) {
	fixture := newFinalizeFixture(t, testHead)
	finalized, err := fixture.finalizer.Finalize(context.Background(), fixture.finalizeInput("https://chatgpt.com/g/g-project/repository/c/final-chat"))
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != orchestration.ProvisionProvisioned || finalized.Target == nil || finalized.Target.Path != "/c/final-chat" {
		t.Fatalf("finalized request = %+v", finalized)
	}
	if finalized.ObservedChatGPTURL != "https://chatgpt.com/g/g-project/repository/c/final-chat" || finalized.BoundBindingID == "" || finalized.BoundBindingVersion != 1 {
		t.Fatalf("final evidence = %+v", finalized)
	}
	if !sameStrings(finalized.AttachmentEvidence, fixture.request.Attachments) || finalized.CompletedAt == nil || finalized.CompletionReason != "exact_session_bound" {
		t.Fatalf("completion evidence = %+v", finalized)
	}
	binding, err := fixture.bindings.Get(context.Background(), fixture.project.ID, fixture.request.LaneKey)
	if err != nil {
		t.Fatal(err)
	}
	if binding.BindingID != finalized.BoundBindingID || binding.BindingVersion != 1 || binding.WorkerID != "managed-worker" || binding.Target.Path != "/c/final-chat" || binding.Readiness != "ready" {
		t.Fatalf("binding = %+v", binding)
	}
	listed, err := fixture.service.ListProject(context.Background(), fixture.project.ID)
	if err != nil || len(listed) != 1 || listed[0].BoundBindingID != binding.BindingID || listed[0].ObservedChatGPTURL == "" {
		t.Fatalf("operator read = %+v err=%v", listed, err)
	}
}

func TestFinalizeProvisioningRollsBackOnStaleHeadAndProjectScope(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		storedHead  string
		observedURL string
	}{
		{name: "stale synchronized Head", storedHead: otherHead, observedURL: "https://chatgpt.com/g/g-project/repository/c/final-chat"},
		{name: "conversation outside Project", storedHead: testHead, observedURL: "https://chatgpt.com/c/final-chat"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFinalizeFixture(t, testCase.storedHead)
			if _, err := fixture.finalizer.Finalize(context.Background(), fixture.finalizeInput(testCase.observedURL)); err == nil {
				t.Fatal("finalize unexpectedly succeeded")
			}
			stored, err := fixture.service.Get(context.Background(), fixture.request.ID)
			if err != nil || stored.Status != orchestration.ProvisionSurfaceReady || stored.BoundBindingID != "" || stored.CompletedAt != nil {
				t.Fatalf("request changed after rollback: %+v err=%v", stored, err)
			}
			if _, err := fixture.bindings.Get(context.Background(), fixture.project.ID, fixture.request.LaneKey); !errors.Is(err, browserbinding.ErrNotFound) {
				t.Fatalf("binding survived failed finalize: %v", err)
			}
		})
	}
}

func TestFinalizeProvisioningRollsBackOnBindingVersionConflict(t *testing.T) {
	fixture := newFinalizeFixture(t, testHead)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	bindingID := "existing-binding"
	_, err := fixture.db.ExecContext(context.Background(), `INSERT INTO browser_lane_bindings(
		binding_id,project_id,lane_key,worker_id,target_kind,target_origin,target_path,target_label,
		enabled,binding_version,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,1,1,?,?)`, bindingID, fixture.project.ID, fixture.request.LaneKey, "old-worker",
		browserbinding.TargetKindChatGPTConversation, "https://chatgpt.com", "/c/old", "", now, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.finalizer.Finalize(context.Background(), fixture.finalizeInput("https://chatgpt.com/g/g-project/repository/c/final-chat")); !errors.Is(err, orchestration.ErrConflict) {
		t.Fatalf("binding version conflict = %v", err)
	}
	stored, err := fixture.service.Get(context.Background(), fixture.request.ID)
	if err != nil || stored.Status != orchestration.ProvisionSurfaceReady || stored.BoundBindingID != "" {
		t.Fatalf("request changed after binding conflict: %+v err=%v", stored, err)
	}
	binding, err := fixture.bindings.Get(context.Background(), fixture.project.ID, fixture.request.LaneKey)
	if err != nil || binding.BindingID != bindingID || binding.BindingVersion != 1 || binding.WorkerID != "old-worker" {
		t.Fatalf("existing binding changed: %+v err=%v", binding, err)
	}
}

type finalizeFixture struct {
	db        *sql.DB
	project   supervisor.Project
	service   *orchestration.ProvisioningService
	bindings  *browserbinding.Service
	finalizer *orchestration.ProvisioningFinalizer
	request   orchestration.ProvisionRequest
	claimed   orchestration.ProvisionRequest
	target    browserbinding.TargetRef
}

func newFinalizeFixture(t *testing.T, persistedHead string) finalizeFixture {
	t.Helper()
	db, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 2, 2, 2)
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		MaxActiveWorkUnits: 2, MaxParallelImplementors: 2, MaxParallelQA: 2,
		ChatGPTProjectURL: finalProjectURL,
	}); err != nil {
		t.Fatal(err)
	}
	intent := schedulerIntent(project.ID, commandID, "finalize-qa", orchestration.ActionDispatch, 101, "qa", 30, fmt.Sprintf("project:%d:issue:101:qa:%s", project.ID, testHead))
	intent.ExpectedHead = testHead
	intent.PRNumber = 150
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{intent})
	decision := claim(t, scheduler, project.ID, "schedule-finalize", snapshot)
	service := provisioningService(t, store)
	request, err := service.Enqueue(context.Background(), orchestration.EnqueueProvisioningInput{
		ProjectID: project.ID, LeaseID: decision.Lease.ID, LeaseOwner: "scheduler", LeaseToken: decision.Lease.LeaseToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimNext(context.Background(), orchestration.ProvisionClaimInput{ClaimID: "finalize-claim", ClaimOwner: "extension", ClaimTTL: time.Minute})
	if err != nil || claimed == nil {
		t.Fatalf("claim = %+v err=%v", claimed, err)
	}
	target := browserbinding.TargetRef{Kind: browserbinding.TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/final-chat"}
	surface, err := service.Complete(context.Background(), orchestration.ProvisionCompletionInput{
		RequestID: request.ID, ClaimOwner: "extension", ClaimToken: claimed.ClaimToken,
		Outcome: orchestration.ProvisionSurfaceReady, WorkerID: "managed-worker", TabID: 77,
	})
	if err != nil {
		t.Fatal(err)
	}
	request = surface
	if err := persistFinalizeSnapshot(t, db, project.ID, persistedHead); err != nil {
		t.Fatal(err)
	}
	bindings := browserbinding.New(db, time.Minute)
	worker, err := bindings.Register(context.Background(), browserbinding.RegisterInput{
		WorkerID: "managed-worker", SessionID: "managed-session", ProtocolVersion: "m11-c3",
		Capabilities: []string{"managed_exact_tab", "library_bootstrap"}, Observation: browserbinding.Observation{Target: &target},
	})
	if err != nil || worker.State != "live" {
		t.Fatalf("register worker = %+v err=%v", worker, err)
	}
	finalizer, err := orchestration.NewProvisioningFinalizer(store, bindings)
	if err != nil {
		t.Fatal(err)
	}
	return finalizeFixture{db: db, project: project, service: service, bindings: bindings, finalizer: finalizer, request: request, claimed: *claimed, target: target}
}

func (f finalizeFixture) finalizeInput(observedURL string) orchestration.FinalizeProvisioningInput {
	return orchestration.FinalizeProvisioningInput{
		RequestID: f.request.ID, ClaimOwner: "extension", ClaimToken: f.claimed.ClaimToken,
		WorkerID: "managed-worker", TabID: 77, Target: f.target, ObservedChatGPTURL: observedURL,
		AttachmentEvidence: append([]string(nil), f.request.Attachments...),
	}
}

func persistFinalizeSnapshot(t *testing.T, db *sql.DB, projectID int64, head string) error {
	t.Helper()
	now := time.Now().UTC()
	return supervisor.NewStore(db).ReplaceSnapshot(context.Background(), projectID, supervisor.RepositorySnapshot{
		FetchedAt: now,
		Issues: []supervisor.Issue{
			{GitHubID: 9001, Number: 90, Title: "Control", State: "open", CreatedAt: now, UpdatedAt: now},
			{
				GitHubID: 9101, Number: 101, Title: "Candidate", State: "open", CreatedAt: now, UpdatedAt: now,
				PullRequests: []supervisor.PullRequest{{
					GitHubID: 9150, Number: 150, Title: "Candidate", State: "open", Draft: true,
					BaseRef: "main", HeadRef: "change", HeadSHA: head, UpdatedAt: now,
					CI: supervisor.CISummary{HeadSHA: head, Status: "completed", Conclusion: "success", Source: "checks", UpdatedAt: now},
				}},
			},
		},
	})
}
