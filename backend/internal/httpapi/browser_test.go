package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func TestBrowserBindingRejectsProjectWorkUnitAndLaneMismatch(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := supervisor.NewStore(db)
	project, err := store.CreateProject(ctx, supervisor.CreateProjectInput{Owner: "acme", Repository: "service", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	if err := store.ReplaceSnapshot(ctx, project.ID, supervisor.RepositorySnapshot{FetchedAt: now, Issues: []supervisor.Issue{{GitHubID: 7, Number: 7, Title: "dispatch", State: "open", URL: "https://example.test/issues/7", Author: "owner", CreatedAt: now, UpdatedAt: now}}}); err != nil {
		t.Fatal(err)
	}
	handler := New(db, store, nil, time.Minute)
	projectID := strconv.FormatInt(project.ID, 10)

	for _, test := range []struct {
		name, path string
		body       []byte
		want       int
	}{
		{name: "unknown project", path: "/api/projects/999/work-units/7/browser-binding", want: http.StatusNotFound},
		{name: "unknown work unit", path: "/api/projects/" + projectID + "/work-units/99/browser-binding", want: http.StatusNotFound},
		{name: "stale expected lane", path: "/api/projects/" + projectID + "/work-units/7/browser-binding", body: []byte(`{"expected_lane_key":"acme/service#7:qa","worker_id":"worker","target":{"kind":"chatgpt_conversation","origin":"https://chatgpt.com","path":"/c/conversation"}}`), want: http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, test.path, bytes.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
