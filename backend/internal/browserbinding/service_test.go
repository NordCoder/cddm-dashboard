package browserbinding

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

func TestPresenceTokensInvalidateForConflictNavigationAndStaleness(t *testing.T) {
	ctx := context.Background()
	db := openBindingDB(t, 1)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	s := New(db, 30*time.Second)
	s.now = func() time.Time { return now }
	target := testTarget("conversation-1")
	if _, err := s.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	binding, err := s.Put(ctx, 1, "acme/service#1:implementor", PutInput{WorkerID: "worker", Target: target})
	if err != nil || binding.Readiness != "ready" || binding.PresenceToken == "" {
		t.Fatalf("initial binding = %#v, err=%v", binding, err)
	}
	initialToken := binding.PresenceToken

	if _, err := s.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	idempotent, err := s.Get(ctx, 1, binding.LaneKey)
	if err != nil || idempotent.Readiness != "ready" || idempotent.PresenceToken != initialToken {
		t.Fatalf("idempotent registration = %#v, err=%v", idempotent, err)
	}

	if _, err := s.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "two", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	conflict, err := s.Get(ctx, 1, binding.LaneKey)
	if err != nil || conflict.Readiness != "conflict" || conflict.PresenceToken != "" {
		t.Fatalf("conflict = %#v, err=%v", conflict, err)
	}

	now = now.Add(20 * time.Second)
	if _, err := s.Heartbeat(ctx, "worker", RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Second)
	if _, err := s.Heartbeat(ctx, "worker", RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	takeover, err := s.Get(ctx, 1, binding.LaneKey)
	if err != nil || takeover.Readiness != "ready" || takeover.PresenceToken == "" || takeover.PresenceToken == initialToken {
		t.Fatalf("takeover = %#v, err=%v", takeover, err)
	}
	takeoverToken := takeover.PresenceToken

	if _, err := s.Heartbeat(ctx, "worker", RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{}}); err != nil {
		t.Fatal(err)
	}
	unavailable, err := s.Get(ctx, 1, binding.LaneKey)
	if err != nil || unavailable.Readiness != "unavailable" || unavailable.PresenceToken != "" {
		t.Fatalf("unavailable = %#v, err=%v", unavailable, err)
	}
	if _, err := s.Heartbeat(ctx, "worker", RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	returned, err := s.Get(ctx, 1, binding.LaneKey)
	if err != nil || returned.Readiness != "ready" || returned.PresenceToken == "" || returned.PresenceToken == takeoverToken {
		t.Fatalf("returned = %#v, err=%v", returned, err)
	}

	now = now.Add(31 * time.Second)
	stale, err := s.Get(ctx, 1, binding.LaneKey)
	if err != nil || stale.Readiness != "stale" || stale.PresenceToken != "" {
		t.Fatalf("stale = %#v, err=%v", stale, err)
	}
	restarted := New(db, 30*time.Second)
	restarted.now = func() time.Time { return now }
	afterRestart, err := restarted.Get(ctx, 1, binding.LaneKey)
	if err != nil || afterRestart.Readiness != "stale" || afterRestart.PresenceToken != "" {
		t.Fatalf("after restart = %#v, err=%v", afterRestart, err)
	}
	if _, err := restarted.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "restarted", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	postRestart, err := restarted.Get(ctx, 1, binding.LaneKey)
	if err != nil || postRestart.Readiness != "ready" || postRestart.PresenceToken == "" || postRestart.PresenceToken == returned.PresenceToken {
		t.Fatalf("post restart = %#v, err=%v", postRestart, err)
	}
	disabled, err := restarted.Disable(ctx, 1, binding.LaneKey, binding.BindingVersion)
	if err != nil || disabled.Readiness != "disabled" || disabled.PresenceToken != "" {
		t.Fatalf("disabled = %#v, err=%v", disabled, err)
	}
}

func TestTargetExclusivityAcrossProjects(t *testing.T) {
	ctx := context.Background()
	db := openBindingDB(t, 2)
	s := New(db, time.Minute)
	target := testTarget("conversation-1")
	if _, err := s.Register(ctx, RegisterInput{WorkerID: "worker", SessionID: "one", Observation: Observation{Target: &target}}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Put(ctx, 1, "acme/service#1:implementor", PutInput{WorkerID: "worker", Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, 2, "acme/service-2#1:implementor", PutInput{WorkerID: "worker", Target: target}); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-project target bind = %v, want conflict", err)
	}
	if _, err := s.Disable(ctx, 1, first.LaneKey, first.BindingVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, 2, "acme/service-2#1:implementor", PutInput{WorkerID: "worker", Target: target}); err != nil {
		t.Fatalf("bind after explicit disable: %v", err)
	}
}

func openBindingDB(t *testing.T, projects int) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for project := 1; project <= projects; project++ {
		if _, err := db.Exec(fmt.Sprintf(`INSERT INTO projects(owner,repository,workflow_mode,polling_enabled,poll_interval_seconds,created_at,updated_at) VALUES('acme','service-%d','pull_request',1,60,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, project)); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func testTarget(key string) TargetRef {
	return TargetRef{Kind: TargetKindChatGPTConversation, Origin: "https://chatgpt.com", Path: "/c/" + key}
}
