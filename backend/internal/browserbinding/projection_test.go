package browserbinding

import (
	"context"
	"testing"
	"time"
)

func TestListWorkerProjectionsExposesOnlyCurrentNormalizedTarget(t *testing.T) {
	ctx := context.Background()
	db := openBindingDB(t, 1)
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	s := New(db, 30*time.Second)
	s.now = func() time.Time { return now }
	target := testTarget("conversation-1")

	if _, err := s.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "session", ProtocolVersion: "m6", Capabilities: []string{"exact_prompt_send"}, Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	workers, err := s.ListWorkerProjections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 1 || workers[0].State != "live" || workers[0].Target == nil || *workers[0].Target != target {
		t.Fatalf("live projection = %#v", workers)
	}
	if workers[0].Target.Label != "" {
		t.Fatalf("projection leaked target label: %#v", workers[0].Target)
	}

	if _, err := s.Heartbeat(ctx, "worker", RegisterInput{WorkerID: "worker", SessionID: "session", Observation: Observation{}}); err != nil {
		t.Fatal(err)
	}
	workers, err = s.ListWorkerProjections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if workers[0].Target != nil || workers[0].State != "live" {
		t.Fatalf("no-target projection = %#v", workers[0])
	}

	now = now.Add(31 * time.Second)
	workers, err = s.ListWorkerProjections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if workers[0].State != "stale" || workers[0].Target != nil {
		t.Fatalf("stale projection = %#v", workers[0])
	}
}
