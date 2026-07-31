package browserbinding

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestWithFreshTargetSessionHoldsPresenceSnapshotThroughCallback(t *testing.T) {
	ctx := context.Background()
	db := openBindingDB(t, 1)
	service := New(db, time.Minute)
	target := testTarget("locked-session")
	if _, err := service.Register(ctx, RegisterInput{
		WorkerID: "worker", SessionID: "session-one", Observation: Observation{Target: &target},
	}); err != nil {
		t.Fatal(err)
	}

	called := false
	err := service.WithFreshTargetSession("worker", target, func(sessionID string) error {
		called = true
		if sessionID != "session-one" {
			return fmt.Errorf("session = %q, want session-one", sessionID)
		}
		if service.mu.TryLock() {
			service.mu.Unlock()
			return fmt.Errorf("presence mutex was not held during durable callback")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("session callback was not called")
	}
	if !service.mu.TryLock() {
		t.Fatal("presence mutex was not released after callback")
	}
	service.mu.Unlock()
}
