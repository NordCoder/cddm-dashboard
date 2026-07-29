package orchestration_test

import (
	"context"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
)

func TestContinuousAutopilotThreeIssueSoakReleasesNextQAAfterCapacity(t *testing.T) {
	ctx := context.Background()
	_, project, store, scheduler, snapshot, commandID := schedulerFixture(t, ":memory:", 3, 3, 1)
	qaFirst := schedulerIntent(project.ID, commandID, "soak-qa-101", orchestration.ActionDispatch, 101, "qa", 10, "project:1:issue:101:qa:first")
	qaSecond := schedulerIntent(project.ID, commandID, "soak-qa-102-release", orchestration.ActionDispatch, 102, "qa", 20, "project:1:issue:102:qa:second")
	qaSecond.ExpectedHead = otherHead
	implementThird := schedulerIntent(project.ID, commandID, "soak-impl-103-spare-wip", orchestration.ActionDispatch, 103, "implementor", 30, "project:1:issue:103:implementor")
	createSchedulerIntents(t, store, project.ID, commandID, []orchestration.IntentInput{qaFirst, qaSecond, implementThird})

	first := claim(t, scheduler, project.ID, "qa-limit-first", snapshot)
	if first.Intent.ID != qaFirst.ID || first.Intent.ExpectedHead != testHead || first.Intent.LaneKey != qaFirst.LaneKey {
		t.Fatalf("first QA identity = %+v", first.Intent)
	}
	spareWIP := claim(t, scheduler, project.ID, "qa-limit-spare-wip", snapshot)
	if spareWIP.Intent.ID != implementThird.ID || spareWIP.Intent.Role != "implementor" {
		t.Fatalf("QA limit did not leave spare Project WIP usable = %+v", spareWIP.Intent)
	}
	blocked, err := scheduler.ClaimNext(ctx, orchestration.ClaimRequest{
		ProjectID: project.ID, ClaimID: "qa-limit-blocked", LeaseOwner: "scheduler", LeaseTTL: time.Minute, Snapshot: snapshot,
	})
	if err != nil || blocked.Claimed || blocked.Reason != "no_runnable_intent" {
		t.Fatalf("second QA must be blocked only by max_parallel_qa = %+v err=%v", blocked, err)
	}
	if _, err := scheduler.Transition(ctx, orchestration.LeaseTransition{
		ProjectID: project.ID, LeaseID: first.Lease.ID, LeaseOwner: first.Lease.LeaseOwner,
		LeaseToken: first.Lease.LeaseToken, Target: orchestration.LeaseCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	released := claim(t, scheduler, project.ID, "qa-limit-released", snapshot)
	if released.Intent.ID != qaSecond.ID || released.Intent.IssueNumber != 102 || released.Intent.ExpectedHead != otherHead || released.Intent.LaneKey != qaSecond.LaneKey {
		t.Fatalf("next exact QA lane was not released after QA capacity = %+v", released.Intent)
	}
}
