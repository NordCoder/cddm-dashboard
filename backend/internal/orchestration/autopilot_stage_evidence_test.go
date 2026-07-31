package orchestration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
)

func TestOperationsProjectionExposesClaimedAndStandaloneProvisionedEvidence(t *testing.T) {
	ctx := context.Background()
	db, project, store, scheduler, _, sourceCommandID := schedulerFixture(t, ":memory:", 3, 3, 3)
	if _, err := store.UpdateProjectProfile(ctx, orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
		DeliveryMode: "auto", MaxActiveWorkUnits: 3, MaxParallelImplementors: 3, MaxParallelQA: 3,
		ChatGPTProjectURL: finalProjectURL,
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := persistRestartSoakSnapshot(t, db, project)

	claimedInput := schedulerIntent(project.ID, sourceCommandID, "stage-claimed", orchestration.ActionDispatch, 101, "implementor", 10, fmt.Sprintf("project:%d:issue:101:implementor", project.ID))
	provisionedInput := schedulerIntent(project.ID, sourceCommandID, "stage-provisioned", orchestration.ActionDispatch, 102, "implementor", 20, fmt.Sprintf("project:%d:issue:102:implementor", project.ID))
	createSchedulerIntents(t, store, project.ID, sourceCommandID, []orchestration.IntentInput{claimedInput, provisionedInput})

	claimed := claimRestartAutopilot(t, scheduler, project.ID, "stage-claimed", snapshot)
	if claimed.Intent.ID != claimedInput.ID {
		t.Fatalf("claimed stage Intent = %+v", claimed.Intent)
	}
	provisioned := claimRestartAutopilot(t, scheduler, project.ID, "stage-provisioned", snapshot)
	if provisioned.Intent.ID != provisionedInput.ID {
		t.Fatalf("provisioned stage Intent = %+v", provisioned.Intent)
	}

	provisions := provisioningService(t, store)
	bindings := browserbinding.New(db, time.Minute)
	finalizer, err := orchestration.NewProvisioningFinalizer(store, bindings)
	if err != nil {
		t.Fatal(err)
	}
	finalized := finalizeSoakProvision(t, provisions, finalizer, bindings, project.ID, *provisioned.Lease, "standalone", 81)

	status, err := orchestration.NewOperationsService(store).Status(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Commands) != 0 {
		t.Fatalf("standalone stages unexpectedly materialized commands: %+v", status.Commands)
	}

	leases := make(map[string]orchestration.LeaseProjection, len(status.Leases))
	for _, lease := range status.Leases {
		leases[lease.IntentID] = lease
	}
	claimedLease, ok := leases[claimedInput.ID]
	if !ok || claimedLease.ID != claimed.Lease.ID || claimedLease.ProjectID != project.ID || claimedLease.ClaimID != "stage-claimed" || claimedLease.LeaseOwner != "dashboard-autopilot" || claimedLease.Status != orchestration.LeaseActive || claimedLease.AcquiredAt.IsZero() || claimedLease.ExpiresAt.IsZero() {
		t.Fatalf("claimed evidence = %+v", claimedLease)
	}
	provisionedLease, ok := leases[provisionedInput.ID]
	if !ok || provisionedLease.ID != provisioned.Lease.ID || provisionedLease.ClaimID != "stage-provisioned" || provisionedLease.LeaseOwner != "dashboard-autopilot" {
		t.Fatalf("provisioned lease evidence = %+v", provisionedLease)
	}

	var request orchestration.ProvisioningProjection
	for _, value := range status.Provisioning {
		if value.IntentID == provisionedInput.ID {
			request = value
		}
	}
	if request.ID != finalized.ID || request.LeaseID != provisioned.Lease.ID || request.WorkerID != finalized.WorkerID || request.WorkerSessionID != finalized.WorkerSessionID || request.TabID != finalized.TabID || request.BoundBindingID != finalized.BoundBindingID || request.BoundBindingVersion != finalized.BoundBindingVersion || request.Status != orchestration.ProvisionProvisioned {
		t.Fatalf("standalone provisioned evidence = %+v, finalized=%+v", request, finalized)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `INSERT INTO autonomous_command_materializations(
		id,project_id,intent_id,lease_id,provision_request_id,scheduler_lane_key,status,created_at,updated_at
	) VALUES(?,?,?,?,?,?,'pending',?,?)`,
		"materialization-pending-stage", project.ID, provisionedInput.ID, provisioned.Lease.ID,
		finalized.ID, provisionedInput.LaneKey, now, now); err != nil {
		t.Fatal(err)
	}

	status, err = orchestration.NewOperationsService(store).Status(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Commands) != 1 {
		t.Fatalf("pending command projection = %+v", status.Commands)
	}
	command := status.Commands[0]
	if command.Status != orchestration.AutonomousMaterializationPending || command.DeliveryCommandID != "" || command.WorkerSessionID != finalized.WorkerSessionID || command.WorkerID != finalized.WorkerID || command.BindingID != finalized.BoundBindingID || command.BindingVersion != finalized.BoundBindingVersion {
		t.Fatalf("pending materialization lost provisioning-owned session evidence: %+v, finalized=%+v", command, finalized)
	}
}
