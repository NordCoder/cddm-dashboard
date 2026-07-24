package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

type PlanningService interface {
	Generate(context.Context, int64, int, string) (planning.GenerationResult, error)
	Latest(context.Context, int64, int) (planning.GenerationResult, error)
	Get(context.Context, int64, int, int64) (planning.GenerationResult, error)
	History(context.Context, int64, int, int) ([]planning.GenerationResult, error)
	ContextSummary(context.Context, int64, int) (planning.ContextSummary, error)
	Health(context.Context) planning.Health
}

type planningHandler struct {
	legacy   http.Handler
	planning PlanningService
}

type generatePlanRequest struct {
	Mode string `json:"mode"`
}

func NewWithPlanning(db *sql.DB, store *supervisor.Store, syncer ProjectSyncer, defaultPollInterval time.Duration, planningService PlanningService) http.Handler {
	return &planningHandler{
		legacy:   New(db, store, syncer, defaultPollInterval),
		planning: planningService,
	}
}

func (h *planningHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/planner/health" {
		h.plannerHealth(w, r)
		return
	}
	if h.planning != nil && h.projectPlanning(w, r) {
		return
	}
	h.legacy.ServeHTTP(w, r)
}

func (h *planningHandler) plannerHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if h.planning == nil {
		writeJSON(w, http.StatusServiceUnavailable, planning.Health{Enabled: false, Status: "unavailable", Runtime: "opencode", Error: "planning service is unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	health := h.planning.Health(ctx)
	status := http.StatusOK
	if health.Enabled && health.Status != "healthy" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, health)
}

func (h *planningHandler) projectPlanning(w http.ResponseWriter, r *http.Request) bool {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[1] != "work-units" {
		return false
	}
	projectID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || projectID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid project id"))
		return true
	}
	issueNumber, err := strconv.Atoi(parts[2])
	if err != nil || issueNumber <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid issue number"))
		return true
	}

	switch {
	case len(parts) == 4 && parts[3] == "plans":
		h.plans(w, r, projectID, issueNumber)
	case len(parts) == 5 && parts[3] == "plans" && parts[4] == "latest":
		h.latestPlan(w, r, projectID, issueNumber)
	case len(parts) == 5 && parts[3] == "plans":
		generationID, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil || generationID <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid plan id"))
			return true
		}
		h.plan(w, r, projectID, issueNumber, generationID)
	case len(parts) == 5 && parts[3] == "planning" && parts[4] == "context":
		h.contextSummary(w, r, projectID, issueNumber)
	case len(parts) == 5 && parts[3] == "planning" && parts[4] == "policy":
		h.latestPolicy(w, r, projectID, issueNumber)
	default:
		return false
	}
	return true
}

func (h *planningHandler) plans(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int) {
	switch r.Method {
	case http.MethodPost:
		var request generatePlanRequest
		if err := decodeOptionalJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		request.Mode = strings.TrimSpace(request.Mode)
		if request.Mode != "" && request.Mode != planning.ModeOpenCode && request.Mode != planning.ModeFallback {
			writeError(w, http.StatusBadRequest, fmt.Errorf("mode must be opencode or fallback"))
			return
		}
		result, err := h.planning.Generate(r.Context(), projectID, issueNumber, request.Mode)
		if h.writePlanningError(w, err) {
			return
		}
		writeJSON(w, http.StatusCreated, result)
	case http.MethodGet:
		limit := 20
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > 100 {
				writeError(w, http.StatusBadRequest, fmt.Errorf("limit must be between 1 and 100"))
				return
			}
			limit = parsed
		}
		history, err := h.planning.History(r.Context(), projectID, issueNumber, limit)
		if h.writePlanningError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plans": history})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h *planningHandler) latestPlan(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	result, err := h.planning.Latest(r.Context(), projectID, issueNumber)
	if h.writePlanningError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *planningHandler) plan(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int, generationID int64) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	result, err := h.planning.Get(r.Context(), projectID, issueNumber, generationID)
	if h.writePlanningError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *planningHandler) contextSummary(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	result, err := h.planning.ContextSummary(r.Context(), projectID, issueNumber)
	if h.writePlanningError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *planningHandler) latestPolicy(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	result, err := h.planning.Latest(r.Context(), projectID, issueNumber)
	if h.writePlanningError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan_id": result.PlanID, "status": result.Status, "policy_decision": result.PolicyDecision})
}

func (h *planningHandler) writePlanningError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if planning.IsNotFound(err) {
		writeError(w, http.StatusNotFound, err)
		return true
	}
	writeError(w, http.StatusInternalServerError, err)
	return true
}

func decodeOptionalJSON(r *http.Request, destination any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain one JSON object")
	}
	return nil
}
