package browserbinding

import (
	"context"
	"testing"
	"time"
)

func TestWorkerProjectionLastSeenIsImmutableSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openBindingDB(t, 1)
	now := time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC)
	s := New(db, 30*time.Second)
	s.now = func() time.Time { return now }
	target := testTarget("conversation-1")
	input := RegisterInput{WorkerID: "worker", SessionID: "session", Observation: Observation{Target: &target}}
	if _, err := s.Register(ctx, input); err != nil {
		t.Fatal(err)
	}
	workers, err := s.ListWorkerProjections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 1 || workers[0].LastSeen == nil {
		t.Fatalf("projection = %#v", workers)
	}
	first := *workers[0].LastSeen
	now = now.Add(time.Second)
	if _, err := s.Heartbeat(ctx, "worker", input); err != nil {
		t.Fatal(err)
	}
	if !workers[0].LastSeen.Equal(first) {
		t.Fatalf("previous projection mutated from %s to %s", first, workers[0].LastSeen)
	}
}
