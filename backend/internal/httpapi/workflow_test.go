package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

func TestDerivedWorkflowEndpoints(t *testing.T) {
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := supervisor.NewStore(db)
	project, err := store.CreateProject(context.Background(), supervisor.CreateProjectInput{
		Owner: "Acme", Repository: "Service", WorkflowMode: "pull_request",
		PollingEnabled: true, PollIntervalSeconds: 60,
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	head := strings.Repeat("a", 40)
	if err := store.ReplaceSnapshot(context.Background(), project.ID, supervisor.RepositorySnapshot{
		FetchedAt: now,
		Issues: []supervisor.Issue{{
			GitHubID: 600, Number: 6, Title: "Stage 3", State: "open", URL: "https://example/issues/6",
			Author: "owner", CreatedAt: now, UpdatedAt: now,
			Labels: []supervisor.Label{{Name: "implementation"}},
			Comments: []supervisor.Comment{{
				GitHubID: 800, Author: "worker", URL: "https://example/comments/800", CreatedAt: now, UpdatedAt: now,
				Body: `## Implementor Handoff

Complete.

<!-- supervisor:event
{"v":1,"event":"worker_result","role":"implementor","status":"completed","head":"` + head + `"}
-->`,
			}},
			PullRequests: []supervisor.PullRequest{{
				GitHubID: 700, Number: 7, Title: "Stage 3", State: "open", Draft: true,
				BaseRef: "main", HeadRef: "stage-3", HeadSHA: head, URL: "https://example/pr/7", UpdatedAt: now,
				CI: supervisor.CISummary{HeadSHA: head, Status: "completed", Conclusion: "success", Source: "check_runs", UpdatedAt: now},
			}},
		}},
	}); err != nil {
		t.Fatalf("ReplaceSnapshot() error = %v", err)
	}

	handler := New(db, store, nil, 5*time.Minute)
	projectID := strconv.FormatInt(project.ID, 10)

	t.Run("workspace state", func(t *testing.T) {
		response := performGET(handler, "/api/workspace/state")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		var state workflow.WorkspaceState
		if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(state.Projects) != 1 || state.Projects[0].WorkUnits[0].Route.TargetRole != "qa" {
			t.Fatalf("state = %#v", state)
		}
	})

	t.Run("project state", func(t *testing.T) {
		response := performGET(handler, "/api/projects/"+projectID+"/state")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		var state workflow.ProjectState
		if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if state.Project.ID != project.ID || state.WorkUnits[0].CurrentHead != head {
			t.Fatalf("state = %#v", state)
		}
	})

	t.Run("work unit state", func(t *testing.T) {
		response := performGET(handler, "/api/projects/"+projectID+"/work-units/6/state")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		var state workflow.WorkUnitState
		if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if state.Identity.IssueNumber != 6 || state.Route.LaneKey != "acme/service#6:qa" {
			t.Fatalf("state = %#v", state)
		}
	})

	t.Run("attention queues", func(t *testing.T) {
		for _, endpoint := range []string{"/api/attention", "/api/projects/" + projectID + "/attention"} {
			response := performGET(handler, endpoint)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, body = %s", endpoint, response.Code, response.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode %s: %v", endpoint, err)
			}
			if _, ok := body["attention"]; !ok {
				t.Fatalf("GET %s body = %#v", endpoint, body)
			}
		}
	})

	t.Run("missing work unit", func(t *testing.T) {
		response := performGET(handler, "/api/projects/"+projectID+"/work-units/999/state")
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", response.Code)
		}
	})
}

func TestDerivedWorkflowEndpointsRejectUnsupportedMethods(t *testing.T) {
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	handler := New(db, supervisor.NewStore(db), nil, time.Minute)

	for _, endpoint := range []string{"/api/workspace/state", "/api/attention"} {
		request := httptest.NewRequest(http.MethodPost, endpoint, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s status = %d, want 405", endpoint, response.Code)
		}
	}
}

func performGET(handler http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
