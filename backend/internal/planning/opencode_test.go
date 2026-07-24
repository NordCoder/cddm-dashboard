package planning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenCodePlannerUsesLongRunningServerBoundary(t *testing.T) {
	var sessions, messages, deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "planner" || password != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/global/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"healthy": true, "version": "test"})
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			sessions.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "session-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/session/session-1/message":
			messages.Add(1)
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode request: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if request["agent"] != "prompt-planner" {
				t.Errorf("agent = %#v", request["agent"])
			}
			tools, _ := request["tools"].(map[string]any)
			for name, enabled := range tools {
				if enabled != false {
					t.Errorf("tool %s enabled: %#v", name, enabled)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"info":  map[string]any{"providerID": "provider", "modelID": "model", "cost": 0.001, "tokens": map[string]any{"input": 10, "output": 5}},
				"parts": []map[string]string{{"type": "text", "text": `{"v":1}`}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/session/session-1":
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	planner, err := NewOpenCodePlanner(OpenCodeConfig{
		Enabled: true, Endpoint: server.URL, Provider: "provider", Model: "model", Agent: "prompt-planner",
		Username: "planner", Password: "secret", Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := planner.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	response, err := planner.Plan(context.Background(), PlannerRequest{ContextJSON: []byte(`{"context_hash":"abc"}`)})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if response.Output != `{"v":1}` || response.Provider != "provider" || response.Usage.InputTokens != 10 {
		t.Fatalf("response = %#v", response)
	}
	if sessions.Load() != 1 || messages.Load() != 1 {
		t.Fatalf("calls sessions=%d messages=%d", sessions.Load(), messages.Load())
	}
	deadline := time.Now().Add(time.Second)
	for deletes.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if deletes.Load() != 1 {
		t.Fatalf("session delete calls = %d", deletes.Load())
	}
}

func TestOpenCodePlannerRejectsCredentialsInEndpoint(t *testing.T) {
	_, err := NewOpenCodePlanner(OpenCodeConfig{
		Enabled: true, Endpoint: "http://user:password@localhost:4096", Provider: "p", Model: "m", Agent: "a",
	})
	if err == nil || !strings.Contains(err.Error(), "must not include credentials") {
		t.Fatalf("NewOpenCodePlanner() error = %v", err)
	}
}

func TestOpenCodePlannerTimeoutUnavailableAndBudget(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
		}))
		defer server.Close()
		planner, _ := NewOpenCodePlanner(OpenCodeConfig{Enabled: true, Endpoint: server.URL, Provider: "p", Model: "m", Agent: "a", Timeout: 10 * time.Millisecond})
		_, err := planner.Plan(context.Background(), PlannerRequest{ContextJSON: []byte(`{}`)})
		if err == nil || errorCategory(err) != "timeout" {
			t.Fatalf("error = %v, category = %q", err, errorCategory(err))
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		endpoint := server.URL
		server.Close()
		planner, _ := NewOpenCodePlanner(OpenCodeConfig{Enabled: true, Endpoint: endpoint, Provider: "p", Model: "m", Agent: "a", Timeout: 100 * time.Millisecond})
		_, err := planner.Plan(context.Background(), PlannerRequest{ContextJSON: []byte(`{}`)})
		if err == nil || errorCategory(err) != "unavailable" {
			t.Fatalf("error = %v, category = %q", err, errorCategory(err))
		}
	})

	t.Run("budget", func(t *testing.T) {
		planner, _ := NewOpenCodePlanner(OpenCodeConfig{Enabled: true, Endpoint: "http://127.0.0.1:1", Provider: "p", Model: "m", Agent: "a", MaxRequestBytes: 4})
		_, err := planner.Plan(context.Background(), PlannerRequest{ContextJSON: []byte(strings.Repeat("x", 5))})
		if err == nil || errorCategory(err) != "budget_rejected" {
			t.Fatalf("error = %v, category = %q", err, errorCategory(err))
		}
	})
}
