package httpapi

import (
	"bytes"
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
)

type apiFakeClient struct {
	snapshot supervisor.RepositorySnapshot
}

func (f apiFakeClient) Snapshot(context.Context, string, string) (supervisor.RepositorySnapshot, error) {
	return f.snapshot, nil
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store := supervisor.NewStore(db)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	client := apiFakeClient{snapshot: supervisor.RepositorySnapshot{
		FetchedAt: now,
		Issues: []supervisor.Issue{{
			GitHubID: 10, Number: 4, Title: "Stage 2", State: "open", URL: "https://example/issues/4",
			Author: "owner", CreatedAt: now, UpdatedAt: now,
			Labels: []supervisor.Label{}, Comments: []supervisor.Comment{}, PullRequests: []supervisor.PullRequest{},
		}},
	}}
	service := supervisor.NewService(store, client, time.Second, 1)
	return New(db, store, service, 5*time.Minute)
}

func TestHealthEndpoint(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body healthResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "healthy" || body.Database != "connected" {
		t.Fatalf("body = %#v", body)
	}
}

func TestProjectAPIAndManualSync(t *testing.T) {
	handler := newTestHandler(t)

	badRequest := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{
		"owner":"acme","repository":"service","github_token":"must-not-be-accepted"
	}`))
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, badRequest)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("credential-bearing request status = %d, want 400", badResponse.Code)
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewBufferString(`{
		"owner":"acme","repository":"service","workflow_mode":"pull_request",
		"polling_enabled":true,"poll_interval_seconds":60
	}`))
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResponse.Code, createResponse.Body.String())
	}
	rawCreateBody := createResponse.Body.Bytes()
	var project supervisor.Project
	if err := json.Unmarshal(rawCreateBody, &project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	if project.Owner != "acme" || project.Repository != "service" || project.PollIntervalSeconds != 60 {
		t.Fatalf("project = %#v", project)
	}
	if strings.Contains(string(rawCreateBody), "token") {
		t.Fatalf("project response contains credential field: %s", rawCreateBody)
	}

	syncRequest := httptest.NewRequest(http.MethodPost, "/api/projects/"+jsonNumber(project.ID)+"/sync", nil)
	syncResponse := httptest.NewRecorder()
	handler.ServeHTTP(syncResponse, syncRequest)
	if syncResponse.Code != http.StatusOK {
		t.Fatalf("sync status = %d, body = %s", syncResponse.Code, syncResponse.Body.String())
	}
	var snapshot supervisor.ProjectSnapshot
	if err := json.NewDecoder(syncResponse.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode sync snapshot: %v", err)
	}
	if snapshot.Project.SyncStatus != "healthy" || len(snapshot.Issues) != 1 || snapshot.Issues[0].Number != 4 {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	for _, endpoint := range []string{"/api/projects", "/api/projects/" + jsonNumber(project.ID), "/api/workspace"} {
		request := httptest.NewRequest(http.MethodGet, endpoint, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", endpoint, response.Code, response.Body.String())
		}
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/projects/"+jsonNumber(project.ID), nil)
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteResponse.Code)
	}
	getDeleted := httptest.NewRequest(http.MethodGet, "/api/projects/"+jsonNumber(project.ID), nil)
	getDeletedResponse := httptest.NewRecorder()
	handler.ServeHTTP(getDeletedResponse, getDeleted)
	if getDeletedResponse.Code != http.StatusNotFound {
		t.Fatalf("deleted project status = %d, want 404", getDeletedResponse.Code)
	}
}

func TestProjectAPIRejectsUnsupportedMethod(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodPut, "/api/projects", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.Code)
	}
	if allow := response.Header().Get("Allow"); allow != "GET, POST" {
		t.Fatalf("Allow = %q", allow)
	}
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}
