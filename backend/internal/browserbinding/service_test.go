package browserbinding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
)

func TestNormalizeTargetRejectsNonIdentityURLData(t *testing.T) {
	valid := TargetRef{Kind: TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/conversation-1"}
	if _, err := NormalizeTarget(valid); err != nil {
		t.Fatalf("NormalizeTarget(valid): %v", err)
	}
	for _, target := range []TargetRef{
		{Kind: TargetKindChatGPTConversation, Origin: "https://chatgpt.com?token=no", Path: "/c/conversation-1"},
		{Kind: TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/conversation-1?token=no"},
		{Kind: TargetKindChatGPTConversation, Origin: "https://evil.example", Path: "/c/conversation-1"},
		{Kind: TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/share/conversation-1"},
	} {
		if _, err := NormalizeTarget(target); !errors.Is(err, ErrInvalid) {
			t.Errorf("NormalizeTarget(%#v) error = %v, want invalid", target, err)
		}
	}
}

func TestBindingLivenessCASAndRestart(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO projects(owner,repository,workflow_mode,polling_enabled,poll_interval_seconds,created_at,updated_at) VALUES('acme','service','pull_request',1,60,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	service := New(db, 30*time.Second)
	service.now = func() time.Time { return now }
	target := TargetRef{Kind: TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/conversation-1"}
	if _, err := service.Register(ctx, RegisterInput{WorkerID: "worker-1", SessionID: "session-1", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	created, err := service.Put(ctx, 1, "acme/service#1:implementor", PutInput{WorkerID: "worker-1", Target: target})
	if err != nil || created.BindingVersion != 1 || created.Readiness != "ready" || created.PresenceToken == "" {
		t.Fatalf("created = %#v, err=%v", created, err)
	}
	if _, err := service.Put(ctx, 1, "acme/service#1:implementor", PutInput{ExpectedVersion: &created.BindingVersion, WorkerID: "worker-1", Target: target}); err != nil {
		t.Fatalf("idempotent rebind: %v", err)
	}
	if _, err := service.Disable(ctx, 1, "acme/service#1:implementor", created.BindingVersion); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale disable = %v, want conflict", err)
	}
	current, err := service.Get(ctx, 1, "acme/service#1:implementor")
	if err != nil {
		t.Fatal(err)
	}
	if current.BindingVersion != 2 {
		t.Fatalf("version=%d, want 2", current.BindingVersion)
	}
	now = now.Add(31 * time.Second)
	stale, err := service.Get(ctx, 1, "acme/service#1:implementor")
	if err != nil || stale.Readiness != "stale" || stale.PresenceToken != "" {
		t.Fatalf("stale=%#v, err=%v", stale, err)
	}
	restarted := New(db, 30*time.Second)
	restarted.now = func() time.Time { return now }
	afterRestart, err := restarted.Get(ctx, 1, "acme/service#1:implementor")
	if err != nil || afterRestart.Readiness != "stale" {
		t.Fatalf("restart=%#v, err=%v", afterRestart, err)
	}
	if _, err := restarted.Register(ctx, RegisterInput{WorkerID: "worker-1", SessionID: "session-2", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	ready, err := restarted.Get(ctx, 1, "acme/service#1:implementor")
	if err != nil || ready.Readiness != "ready" || ready.PresenceToken == created.PresenceToken {
		t.Fatalf("fresh presence=%#v, err=%v", ready, err)
	}
}

func TestWorkerSessionAndTargetConflicts(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO projects(owner,repository,workflow_mode,polling_enabled,poll_interval_seconds,created_at,updated_at) VALUES('acme','service','pull_request',1,60,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	s := New(db, time.Minute)
	target := TargetRef{Kind: TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/conversation-1"}
	if _, err := s.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "two", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, 1, "lane-1", PutInput{WorkerID: "worker", Target: target}); !errors.Is(err, ErrConflict) {
		t.Fatalf("concurrent sessions bind=%v, want conflict", err)
	}
}

func TestTargetCannotBeSharedAndDisableUsesCAS(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO projects(owner,repository,workflow_mode,polling_enabled,poll_interval_seconds,created_at,updated_at) VALUES('acme','service','pull_request',1,60,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	s := New(db, time.Minute)
	target := TargetRef{Kind: TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/conversation-1"}
	if _, err := s.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Put(ctx, 1, "lane-1", PutInput{WorkerID: "worker", Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, 1, "lane-2", PutInput{WorkerID: "worker", Target: target}); !errors.Is(err, ErrConflict) {
		t.Fatalf("shared target bind = %v, want conflict", err)
	}
	if _, err := s.Disable(ctx, 1, "lane-1", first.BindingVersion+1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale disable = %v, want conflict", err)
	}
	disabled, err := s.Disable(ctx, 1, "lane-1", first.BindingVersion)
	if err != nil || disabled.Readiness != "disabled" || disabled.BindingVersion != 2 {
		t.Fatalf("disabled = %#v, err=%v", disabled, err)
	}
	if _, err := s.Put(ctx, 1, "lane-2", PutInput{WorkerID: "worker", Target: target}); err != nil {
		t.Fatalf("bind after explicit disable: %v", err)
	}
}
