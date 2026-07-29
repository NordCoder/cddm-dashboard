package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NordCoder/cddm-dashboard/backend/internal/database"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func TestAutopilotOperationsAPIStatusControlAndConflict(t *testing.T) {
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projects := supervisor.NewStore(db)
	project, err := projects.CreateProject(context.Background(), supervisor.CreateProjectInput{
		Owner: "NordCoder", Repository: "app", WorkflowMode: "pull_request", PollingEnabled: true, PollIntervalSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := orchestration.NewStore(db)
	if _, err := store.UpdateProjectProfile(context.Background(), orchestration.ProjectProfileInput{
		ProjectID: project.ID, AutonomyMode: orchestration.AutonomyModeContinuous,
		AutonomyState: orchestration.AutonomyStateEnabled, ControlIssueNumber: 90,
	}); err != nil {
		t.Fatal(err)
	}
	handler := WithAutopilotOperations(http.NotFoundHandler(), orchestration.NewOperationsService(store))

	get := httptest.NewRequest(http.MethodGet, "/api/projects/"+jsonNumber(project.ID)+"/autopilot", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	var initial orchestration.AutopilotStatus
	if err := json.NewDecoder(getResponse.Body).Decode(&initial); err != nil {
		t.Fatal(err)
	}
	if initial.Control.Revision != 0 || initial.Profile.AutonomyState != orchestration.AutonomyStateEnabled {
		t.Fatalf("initial=%+v", initial)
	}

	pause := httptest.NewRequest(http.MethodPost, "/api/projects/"+jsonNumber(project.ID)+"/autopilot/pause", strings.NewReader(`{"expected_revision":0}`))
	pauseResponse := httptest.NewRecorder()
	handler.ServeHTTP(pauseResponse, pause)
	if pauseResponse.Code != http.StatusOK {
		t.Fatalf("pause status=%d body=%s", pauseResponse.Code, pauseResponse.Body.String())
	}
	var paused orchestration.AutopilotStatus
	if err := json.NewDecoder(pauseResponse.Body).Decode(&paused); err != nil {
		t.Fatal(err)
	}
	if paused.Control.Revision != 1 || paused.Profile.AutonomyState != orchestration.AutonomyStatePaused {
		t.Fatalf("paused=%+v", paused)
	}

	stale := httptest.NewRequest(http.MethodPost, "/api/projects/"+jsonNumber(project.ID)+"/autopilot/stop", strings.NewReader(`{"expected_revision":0}`))
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", staleResponse.Code, staleResponse.Body.String())
	}
}
