package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
)

type autopilotOperationsHandler struct {
	legacy  http.Handler
	service *orchestration.OperationsService
}

type autopilotControlRequest struct {
	ExpectedRevision int64 `json:"expected_revision"`
}

type autopilotBreakerRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	ScopeKind        string `json:"scope_kind"`
	LaneKey          string `json:"lane_key,omitempty"`
	Reason           string `json:"reason"`
	Evidence         string `json:"evidence,omitempty"`
	ExpectedHead     string `json:"expected_head,omitempty"`
}

func WithAutopilotOperations(legacy http.Handler, service *orchestration.OperationsService) http.Handler {
	return &autopilotOperationsHandler{legacy: legacy, service: service}
}

func (h *autopilotOperationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.handle(w, r) {
		return
	}
	h.legacy.ServeHTTP(w, r)
}

func (h *autopilotOperationsHandler) handle(w http.ResponseWriter, r *http.Request) bool {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "autopilot" {
		return false
	}
	projectID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || projectID <= 0 {
		return false
	}
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return true
		}
		value, err := h.service.Status(r.Context(), projectID)
		if h.writeError(w, err) {
			return true
		}
		writeJSON(w, http.StatusOK, value)
		return true
	}
	if len(parts) == 3 {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return true
		}
		var request autopilotControlRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		var value orchestration.AutopilotStatus
		switch parts[2] {
		case "enable":
			value, err = h.service.Enable(r.Context(), projectID, request.ExpectedRevision)
		case "pause":
			value, err = h.service.Pause(r.Context(), projectID, request.ExpectedRevision)
		case "resume":
			value, err = h.service.Resume(r.Context(), projectID, request.ExpectedRevision)
		case "stop":
			value, err = h.service.Stop(r.Context(), projectID, request.ExpectedRevision)
		default:
			return false
		}
		if h.writeError(w, err) {
			return true
		}
		writeJSON(w, http.StatusOK, value)
		return true
	}
	if len(parts) < 4 || parts[2] != "circuit-breakers" || r.Method != http.MethodPost {
		if len(parts) >= 3 && parts[2] == "circuit-breakers" {
			methodNotAllowed(w, http.MethodPost)
			return true
		}
		return false
	}
	if len(parts) == 4 {
		var request autopilotBreakerRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		value, err := h.service.TripBreaker(r.Context(), orchestration.BreakerTripInput{
			ProjectID: projectID, ExpectedRevision: request.ExpectedRevision, ScopeKind: request.ScopeKind,
			LaneKey: request.LaneKey, Code: parts[3], Reason: request.Reason, Evidence: request.Evidence,
			ExpectedHead: request.ExpectedHead,
		})
		if h.writeError(w, err) {
			return true
		}
		writeJSON(w, http.StatusOK, value)
		return true
	}
	if len(parts) == 5 && (parts[4] == "acknowledge" || parts[4] == "resolve") {
		var request autopilotControlRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		input := orchestration.BreakerTransitionInput{ProjectID: projectID, BreakerID: parts[3], ExpectedRevision: request.ExpectedRevision}
		var value orchestration.AutopilotStatus
		if parts[4] == "acknowledge" {
			value, err = h.service.AcknowledgeBreaker(r.Context(), input)
		} else {
			value, err = h.service.ResolveBreaker(r.Context(), input)
		}
		if h.writeError(w, err) {
			return true
		}
		writeJSON(w, http.StatusOK, value)
		return true
	}
	return false
}

func (h *autopilotOperationsHandler) writeError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, orchestration.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, orchestration.ErrConflict):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("Autopilot operation: %w", err))
	}
	return true
}
