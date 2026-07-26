package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
)

type fakePlanningAPI struct {
	mode string
}

type fakeDeliveryAPI struct{ created delivery.Confirmation }

func (f *fakeDeliveryAPI) Create(_ context.Context, projectID int64, issueNumber int, input delivery.Confirmation) (delivery.Command, error) {
	f.created = input
	return delivery.Command{ID: "command", ProjectID: projectID, IssueNumber: issueNumber, Status: delivery.StatusPending}, nil
}
func (f *fakeDeliveryAPI) List(context.Context, int64, int) ([]delivery.Command, error) {
	return []delivery.Command{{ID: "command", Status: delivery.StatusPending}}, nil
}
func (f *fakeDeliveryAPI) ClaimNext(context.Context, delivery.ClaimRequest) (*delivery.Execution, error) {
	return &delivery.Execution{ClaimID: "claim", Prompt: "canonical prompt"}, nil
}
func (f *fakeDeliveryAPI) Complete(_ context.Context, input delivery.Completion) (delivery.Command, error) {
	return delivery.Command{ID: input.CommandID, Status: input.Outcome}, nil
}

func (f *fakePlanningAPI) Generate(_ context.Context, projectID int64, issueNumber int, mode string) (planning.GenerationResult, error) {
	f.mode = mode
	if projectID != 1 || issueNumber != 11 {
		return planning.GenerationResult{}, errors.New("wrong work unit")
	}
	return planning.GenerationResult{Status: planning.StatusFallback, PlanID: 9}, nil
}
func (f *fakePlanningAPI) Latest(context.Context, int64, int) (planning.GenerationResult, error) {
	return planning.GenerationResult{Status: planning.StatusApproved, PlanID: 9, PolicyDecision: planning.PolicyDecision{Status: planning.StatusApproved}}, nil
}
func (f *fakePlanningAPI) Get(context.Context, int64, int, int64) (planning.GenerationResult, error) {
	return planning.GenerationResult{Status: planning.StatusApproved, PlanID: 9}, nil
}
func (f *fakePlanningAPI) History(context.Context, int64, int, int) ([]planning.GenerationResult, error) {
	return []planning.GenerationResult{{Status: planning.StatusApproved, PlanID: 9}}, nil
}
func (f *fakePlanningAPI) ContextSummary(context.Context, int64, int) (planning.ContextSummary, error) {
	return planning.ContextSummary{Version: 1, ContextHash: strings.Repeat("a", 64)}, nil
}
func (f *fakePlanningAPI) Health(context.Context) planning.Health {
	return planning.Health{Enabled: true, Status: "healthy", Runtime: "opencode", Endpoint: "http://opencode:4096", Provider: "provider", Model: "model", Agent: "prompt-planner"}
}

func TestPlanningAPIEndpointsAndExplicitMode(t *testing.T) {
	service := &fakePlanningAPI{}
	handler := &planningHandler{legacy: http.NotFoundHandler(), planning: service}

	request := httptest.NewRequest(http.MethodPost, "/api/projects/1/work-units/11/plans", strings.NewReader(`{"mode":"fallback"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.mode != planning.ModeFallback {
		t.Fatalf("generation response = %d %s; mode=%q", response.Code, response.Body.String(), service.mode)
	}
	var generated planning.GenerationResult
	if err := json.Unmarshal(response.Body.Bytes(), &generated); err != nil || generated.PlanID != 9 {
		t.Fatalf("decode generation = %#v, %v", generated, err)
	}

	for _, endpoint := range []string{
		"/api/projects/1/work-units/11/plans",
		"/api/projects/1/work-units/11/plans/latest",
		"/api/projects/1/work-units/11/plans/9",
		"/api/projects/1/work-units/11/planning/context",
		"/api/projects/1/work-units/11/planning/policy",
		"/api/planner/health",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", endpoint, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "authorization") {
			t.Fatalf("GET %s exposed credential field: %s", endpoint, response.Body.String())
		}
	}
}

func TestPlanningAPIMethodAndInputValidation(t *testing.T) {
	handler := &planningHandler{legacy: http.NotFoundHandler(), planning: &fakePlanningAPI{}}
	for _, test := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPost, "/api/projects/not-a-number/work-units/11/plans", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/projects/1/work-units/11/plans", `{"credentials":"secret"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/projects/1/work-units/11/plans", `{"mode":"direct-provider"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/projects/1/work-units/11/plans", `{"mode":"fallback"}{}`, http.StatusBadRequest},
		{http.MethodDelete, "/api/projects/1/work-units/11/plans", ``, http.StatusMethodNotAllowed},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
		if response.Code != test.status {
			t.Fatalf("%s %s = %d %s, want %d", test.method, test.path, response.Code, response.Body.String(), test.status)
		}
	}
}

func TestBrowserDeliveryContractEndpoints(t *testing.T) {
	service := &fakeDeliveryAPI{}
	handler := &planningHandler{legacy: http.NotFoundHandler(), planning: &fakePlanningAPI{}, delivery: service}
	create := httptest.NewRecorder()
	handler.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/projects/1/work-units/11/deliveries", strings.NewReader(`{"plan_id":9,"idempotency_key":"intent","expected_plan_hash":"plan","expected_context_hash":"context","expected_head":"head","expected_lane_key":"lane","expected_binding_id":"binding","expected_binding_version":1,"expected_presence_token":"presence"}`)))
	if create.Code != http.StatusCreated || service.created.IdempotencyKey != "intent" {
		t.Fatalf("create = %d %s; request=%#v", create.Code, create.Body.String(), service.created)
	}
	claim := httptest.NewRecorder()
	handler.ServeHTTP(claim, httptest.NewRequest(http.MethodPost, "/api/browser/deliveries/claim-next", strings.NewReader(`{"worker_id":"worker","worker_session_id":"session","claim_request_id":"request"}`)))
	if claim.Code != http.StatusOK || !strings.Contains(claim.Body.String(), "canonical prompt") {
		t.Fatalf("claim = %d %s", claim.Code, claim.Body.String())
	}
	complete := httptest.NewRecorder()
	handler.ServeHTTP(complete, httptest.NewRequest(http.MethodPost, "/api/browser/deliveries/command/complete", strings.NewReader(`{"claim_id":"claim","outcome":"delivered"}`)))
	if complete.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", complete.Code, complete.Body.String())
	}
}
