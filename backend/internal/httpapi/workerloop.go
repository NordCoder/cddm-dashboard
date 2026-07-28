package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

type workerLoopHandler struct {
	legacy      http.Handler
	projects    *supervisor.Store
	projections *workerloop.ProjectionService
	bindings    *browserbinding.Service
}

type roleBindingRequest struct {
	ExpectedBindingVersion *int64                   `json:"expected_binding_version,omitempty"`
	WorkerID               string                   `json:"worker_id"`
	Target                 browserbinding.TargetRef `json:"target"`
}

type disableRoleBindingRequest struct {
	ExpectedBindingVersion int64 `json:"expected_binding_version"`
}

func WithWorkerLoop(legacy http.Handler, projects *supervisor.Store, projections *workerloop.ProjectionService, bindings *browserbinding.Service) http.Handler {
	return &workerLoopHandler{legacy: legacy, projects: projects, projections: projections, bindings: bindings}
}

func (h *workerLoopHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.handle(w, r) {
		return
	}
	h.legacy.ServeHTTP(w, r)
}

func (h *workerLoopHandler) handle(w http.ResponseWriter, r *http.Request) bool {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
	parts := strings.Split(path, "/")
	if path == "" {
		return false
	}
	projectID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || projectID <= 0 {
		return false
	}
	if len(parts) == 2 && parts[1] == "execution-profile" {
		h.executionProfile(w, r, projectID)
		return true
	}
	if len(parts) < 4 || parts[1] != "work-units" {
		return false
	}
	issueNumber, err := strconv.Atoi(parts[2])
	if err != nil || issueNumber <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid issue number"))
		return true
	}
	switch {
	case len(parts) == 4 && parts[3] == "execution":
		h.execution(w, r, projectID, issueNumber)
		return true
	case len(parts) == 4 && parts[3] == "pilot-readiness":
		h.pilotReadiness(w, r, projectID, issueNumber)
		return true
	case len(parts) == 5 && parts[3] == "role-bindings":
		h.roleBinding(w, r, projectID, issueNumber, strings.ToLower(parts[4]))
		return true
	default:
		return false
	}
}

func (h *workerLoopHandler) executionProfile(w http.ResponseWriter, r *http.Request, projectID int64) {
	switch r.Method {
	case http.MethodGet:
		profile, err := h.projections.Profile(r.Context(), projectID)
		if h.writeError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, profile)
	case http.MethodPut:
		var profile workerloop.ExecutionProfile
		if err := decodeJSON(r, &profile); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if profile.ProjectID != 0 && profile.ProjectID != projectID {
			writeError(w, http.StatusConflict, fmt.Errorf("project id mismatch"))
			return
		}
		profile.ProjectID = projectID
		updated, err := h.projections.UpdateProfile(r.Context(), profile)
		if h.writeError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

func (h *workerLoopHandler) execution(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	value, err := h.projections.WorkUnit(r.Context(), projectID, issueNumber)
	if h.writeError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h *workerLoopHandler) pilotReadiness(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	value, err := h.projections.Readiness(r.Context(), projectID, issueNumber)
	if h.writeError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h *workerLoopHandler) roleBinding(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int, role string) {
	if !workerloop.ValidRole(role) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("role must be lead, implementor or qa"))
		return
	}
	project, err := h.projects.GetProject(r.Context(), projectID)
	if h.writeError(w, err) {
		return
	}
	lane := workerloop.LogicalLane(project, issueNumber, role)
	switch r.Method {
	case http.MethodGet:
		binding, err := h.bindings.Get(r.Context(), projectID, lane)
		if errors.Is(err, browserbinding.ErrNotFound) {
			writeJSON(w, http.StatusOK, workerloop.RoleBinding{Role: role, LaneKey: lane})
			return
		}
		if h.writeError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, workerloop.RoleBinding{Role: role, LaneKey: lane, Binding: &binding})
	case http.MethodPut:
		var request roleBindingRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		binding, err := h.bindings.Put(r.Context(), projectID, lane, browserbinding.PutInput{
			ExpectedVersion: request.ExpectedBindingVersion, WorkerID: request.WorkerID, Target: request.Target,
		})
		if h.writeError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, workerloop.RoleBinding{Role: role, LaneKey: lane, Binding: &binding})
	case http.MethodDelete:
		var request disableRoleBindingRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		binding, err := h.bindings.Disable(r.Context(), projectID, lane, request.ExpectedBindingVersion)
		if h.writeError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, workerloop.RoleBinding{Role: role, LaneKey: lane, Binding: &binding})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (h *workerLoopHandler) writeError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, supervisor.ErrNotFound), errors.Is(err, browserbinding.ErrNotFound), errors.Is(err, workerloop.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, browserbinding.ErrConflict), errors.Is(err, workerloop.ErrConflict):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
	return true
}
