package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
)

type fakePlanningAPI struct {
	mode string
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
