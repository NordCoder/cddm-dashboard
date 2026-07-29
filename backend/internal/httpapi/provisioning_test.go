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
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

func TestProvisioningAPIProfileQueueAndEmptyClaim(t *testing.T) {
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
	pack, err := resourcepack.Load(resourcepack.V2Profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := orchestration.NewProvisioningService(store, pack)
	if err != nil {
		t.Fatal(err)
	}
	handler := WithProvisioning(http.NotFoundHandler(), store, service)

	profileBody := `{
		"resource_version":"cddm-dashboard-resources/v2.0",
		"methodology_version":"cddm-minimal/v2.1",
		"result_protocol":"cddm-worker-result/v2",
		"delivery_mode":"auto",
		"qa_session_mode":"manual_fresh_binding",
		"auto_merge":false,
		"autonomy_mode":"continuous_dashboard_orchestration",
		"autonomy_state":"enabled",
		"control_issue_number":90,
		"max_active_work_units":3,
		"max_parallel_implementors":2,
		"max_parallel_qa":2,
		"chatgpt_project_url":"https://chatgpt.com/g/g-project/example/project/"
	}`
	put := httptest.NewRequest(http.MethodPut, "/api/projects/"+jsonNumber(project.ID)+"/autonomy-profile", strings.NewReader(profileBody))
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("profile PUT status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	var profile orchestration.ProjectProfile
	if err := json.NewDecoder(putResponse.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	if profile.ChatGPTProjectURL != "https://chatgpt.com/g/g-project/example/project" || profile.AutonomyState != orchestration.AutonomyStateEnabled {
		t.Fatalf("profile = %+v", profile)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/projects/"+jsonNumber(project.ID)+"/autonomy-profile", nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("profile GET status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, "/api/projects/"+jsonNumber(project.ID)+"/session-provisioning", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK || strings.TrimSpace(listResponse.Body.String()) != "[]" {
		t.Fatalf("queue GET status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	claim := httptest.NewRequest(http.MethodPost, "/api/browser/provisioning/claim-next", strings.NewReader(`{
		"claim_request_id":"claim-empty","claim_owner":"extension","claim_ttl_seconds":120
	}`))
	claimResponse := httptest.NewRecorder()
	handler.ServeHTTP(claimResponse, claim)
	if claimResponse.Code != http.StatusNoContent {
		t.Fatalf("empty claim status=%d body=%s", claimResponse.Code, claimResponse.Body.String())
	}
}

func TestProvisioningAPIRejectsInvalidProjectScopeAndMethods(t *testing.T) {
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
	pack, err := resourcepack.Load(resourcepack.V2Profile)
	if err != nil {
		t.Fatal(err)
	}
	service, err := orchestration.NewProvisioningService(store, pack)
	if err != nil {
		t.Fatal(err)
	}
	handler := WithProvisioning(http.NotFoundHandler(), store, service)

	bad := httptest.NewRequest(http.MethodPut, "/api/projects/"+jsonNumber(project.ID)+"/autonomy-profile", strings.NewReader(`{
		"autonomy_mode":"continuous_dashboard_orchestration","autonomy_state":"enabled",
		"control_issue_number":90,"chatgpt_project_url":"https://evil.example/project"
	}`))
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope status=%d body=%s", badResponse.Code, badResponse.Body.String())
	}

	method := httptest.NewRequest(http.MethodDelete, "/api/browser/provisioning/claim-next", nil)
	methodResponse := httptest.NewRecorder()
	handler.ServeHTTP(methodResponse, method)
	if methodResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status=%d", methodResponse.Code)
	}
}
