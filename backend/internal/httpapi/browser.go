package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

type browserHandler struct {
	legacy   http.Handler
	server   *Server
	bindings *browserbinding.Service
}
type bindingRequest struct {
	ExpectedLaneKey string                   `json:"expected_lane_key"`
	ExpectedVersion *int64                   `json:"expected_binding_version"`
	WorkerID        string                   `json:"worker_id"`
	Target          browserbinding.TargetRef `json:"target"`
}
type disableBindingRequest struct {
	ExpectedLaneKey string `json:"expected_lane_key"`
	ExpectedVersion int64  `json:"expected_binding_version"`
}

func withBrowserBinding(legacy http.Handler, server *Server, bindings *browserbinding.Service) http.Handler {
	return &browserHandler{legacy: legacy, server: server, bindings: bindings}
}
func (h *browserHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/browser/workers" {
		h.workers(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/browser/workers/") {
		h.worker(w, r)
		return
	}
	if h.binding(w, r) {
		return
	}
	h.legacy.ServeHTTP(w, r)
}
func (h *browserHandler) workers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		workers, err := h.bindings.ListWorkers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workers": workers})
	case http.MethodPost:
		var in browserbinding.RegisterInput
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		worker, err := h.bindings.Register(r.Context(), in)
		h.writeBrowserResult(w, http.StatusCreated, worker, err)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}
func (h *browserHandler) worker(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/browser/workers/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "heartbeat" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var in browserbinding.RegisterInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	worker, err := h.bindings.Heartbeat(r.Context(), parts[0], in)
	h.writeBrowserResult(w, http.StatusOK, worker, err)
}
func (h *browserHandler) binding(w http.ResponseWriter, r *http.Request) bool {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 4 || parts[1] != "work-units" || parts[3] != "browser-binding" {
		return false
	}
	projectID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || projectID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid project id"))
		return true
	}
	issue, err := strconv.Atoi(parts[2])
	if err != nil || issue <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid issue number"))
		return true
	}
	state, ok := h.server.readProjectState(w, r, projectID)
	if !ok {
		return true
	}
	unit, found := findWorkUnit(state, issue)
	if !found {
		writeError(w, http.StatusNotFound, fmt.Errorf("work unit not found"))
		return true
	}
	lane := unit.Route.LaneKey
	if lane == "" {
		writeError(w, http.StatusConflict, fmt.Errorf("current route has no safe browser-dispatch lane"))
		return true
	}
	switch r.Method {
	case http.MethodGet:
		b, err := h.bindings.Get(r.Context(), projectID, lane)
		if errors.Is(err, browserbinding.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"lane_key": lane, "binding": nil})
			return true
		}
		h.writeBrowserResult(w, http.StatusOK, b, err)
	case http.MethodPut:
		if unit.Route.Action != "dispatch" {
			writeError(w, http.StatusConflict, fmt.Errorf("current route has no safe browser-dispatch lane"))
			return true
		}
		var in bindingRequest
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		if in.ExpectedLaneKey != lane {
			writeError(w, http.StatusConflict, fmt.Errorf("expected lane does not match current route"))
			return true
		}
		b, err := h.bindings.Put(r.Context(), projectID, lane, browserbinding.PutInput{ExpectedVersion: in.ExpectedVersion, WorkerID: in.WorkerID, Target: in.Target})
		h.writeBrowserResult(w, http.StatusOK, b, err)
	case http.MethodDelete:
		if unit.Route.Action != "dispatch" {
			writeError(w, http.StatusConflict, fmt.Errorf("current route has no safe browser-dispatch lane"))
			return true
		}
		var in disableBindingRequest
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		if in.ExpectedLaneKey != lane {
			writeError(w, http.StatusConflict, fmt.Errorf("expected lane does not match current route"))
			return true
		}
		b, err := h.bindings.Disable(r.Context(), projectID, lane, in.ExpectedVersion)
		h.writeBrowserResult(w, http.StatusOK, b, err)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
	return true
}
func findWorkUnit(state workflow.ProjectState, issue int) (workflow.WorkUnitState, bool) {
	return workflow.FindWorkUnit(state, issue)
}
func (h *browserHandler) writeBrowserResult(w http.ResponseWriter, status int, value any, err error) {
	if err == nil {
		writeJSON(w, status, value)
		return
	}
	if errors.Is(err, browserbinding.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
	} else if errors.Is(err, browserbinding.ErrConflict) {
		writeError(w, http.StatusConflict, err)
	} else if errors.Is(err, browserbinding.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err)
	} else {
		writeError(w, http.StatusInternalServerError, err)
	}
}
