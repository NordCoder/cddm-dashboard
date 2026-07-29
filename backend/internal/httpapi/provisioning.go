package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/orchestration"
)

type provisioningHandler struct {
	legacy    http.Handler
	store     *orchestration.Store
	service   *orchestration.ProvisioningService
	finalizer *orchestration.ProvisioningFinalizer
}

type provisionEnqueueRequest struct {
	LeaseID    string `json:"lease_id"`
	LeaseOwner string `json:"lease_owner"`
	LeaseToken string `json:"lease_token"`
}

type provisionClaimRequest struct {
	ClaimID         string `json:"claim_request_id"`
	ClaimOwner      string `json:"claim_owner"`
	ClaimTTLSeconds int64  `json:"claim_ttl_seconds"`
}

type provisionCompleteRequest struct {
	ClaimOwner         string                    `json:"claim_owner"`
	ClaimToken         string                    `json:"claim_token"`
	Outcome            string                    `json:"outcome"`
	Reason             string                    `json:"reason,omitempty"`
	WorkerID           string                    `json:"worker_id,omitempty"`
	TabID              int                       `json:"tab_id,omitempty"`
	Target             *browserbinding.TargetRef `json:"target,omitempty"`
	AttachmentEvidence []string                  `json:"attachment_evidence,omitempty"`
}

type provisionFinalizeRequest struct {
	ClaimOwner         string                   `json:"claim_owner"`
	ClaimToken         string                   `json:"claim_token"`
	WorkerID           string                   `json:"worker_id"`
	TabID              int                      `json:"tab_id"`
	Target             browserbinding.TargetRef `json:"target"`
	ObservedChatGPTURL string                   `json:"observed_chatgpt_url"`
	AttachmentEvidence []string                 `json:"attachment_evidence"`
}

func WithProvisioning(legacy http.Handler, store *orchestration.Store, service *orchestration.ProvisioningService) http.Handler {
	return &provisioningHandler{legacy: legacy, store: store, service: service}
}

func WithProvisioningAndFinalizer(legacy http.Handler, store *orchestration.Store, service *orchestration.ProvisioningService, finalizer *orchestration.ProvisioningFinalizer) http.Handler {
	return &provisioningHandler{legacy: legacy, store: store, service: service, finalizer: finalizer}
}

func (h *provisioningHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.handleBrowser(w, r) || h.handleProject(w, r) {
		return
	}
	h.legacy.ServeHTTP(w, r)
}

func (h *provisioningHandler) handleBrowser(w http.ResponseWriter, r *http.Request) bool {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/browser/provisioning/"), "/")
	if r.URL.Path == "/api/browser/provisioning/claim-next" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return true
		}
		var request provisionClaimRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		if request.ClaimTTLSeconds == 0 {
			request.ClaimTTLSeconds = 120
		}
		value, err := h.service.ClaimNext(r.Context(), orchestration.ProvisionClaimInput{
			ClaimID: request.ClaimID, ClaimOwner: request.ClaimOwner,
			ClaimTTL: time.Duration(request.ClaimTTLSeconds) * time.Second,
		})
		if h.writeError(w, err) {
			return true
		}
		if value == nil {
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		writeJSON(w, http.StatusOK, value)
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" {
		return false
	}
	switch parts[1] {
	case "complete":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return true
		}
		var request provisionCompleteRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		value, err := h.service.Complete(r.Context(), orchestration.ProvisionCompletionInput{
			RequestID: parts[0], ClaimOwner: request.ClaimOwner, ClaimToken: request.ClaimToken,
			Outcome: request.Outcome, Reason: request.Reason, WorkerID: request.WorkerID,
			TabID: request.TabID, Target: request.Target, AttachmentEvidence: request.AttachmentEvidence,
		})
		if h.writeError(w, err) {
			return true
		}
		writeJSON(w, http.StatusOK, value)
		return true
	case "finalize":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return true
		}
		if h.finalizer == nil {
			writeError(w, http.StatusNotImplemented, fmt.Errorf("provisioning finalize is unavailable"))
			return true
		}
		var request provisionFinalizeRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		value, err := h.finalizer.Finalize(r.Context(), orchestration.FinalizeProvisioningInput{
			RequestID: parts[0], ClaimOwner: request.ClaimOwner, ClaimToken: request.ClaimToken,
			WorkerID: request.WorkerID, TabID: request.TabID, Target: request.Target,
			ObservedChatGPTURL: request.ObservedChatGPTURL, AttachmentEvidence: request.AttachmentEvidence,
		})
		if h.writeError(w, err) {
			return true
		}
		writeJSON(w, http.StatusOK, value)
		return true
	default:
		return false
	}
}

func (h *provisioningHandler) handleProject(w http.ResponseWriter, r *http.Request) bool {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || path == "" {
		return false
	}
	projectID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || projectID <= 0 {
		return false
	}
	if len(parts) == 2 && parts[1] == "autonomy-profile" {
		h.autonomyProfile(w, r, projectID)
		return true
	}
	if parts[1] != "session-provisioning" {
		return false
	}
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return true
		}
		values, err := h.service.ListProject(r.Context(), projectID)
		if h.writeError(w, err) {
			return true
		}
		writeJSON(w, http.StatusOK, values)
		return true
	}
	if len(parts) == 3 && parts[2] == "enqueue" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return true
		}
		var request provisionEnqueueRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return true
		}
		value, err := h.service.Enqueue(r.Context(), orchestration.EnqueueProvisioningInput{
			ProjectID: projectID, LeaseID: request.LeaseID, LeaseOwner: request.LeaseOwner, LeaseToken: request.LeaseToken,
		})
		if h.writeError(w, err) {
			return true
		}
		writeJSON(w, http.StatusOK, value)
		return true
	}
	return false
}

func (h *provisioningHandler) autonomyProfile(w http.ResponseWriter, r *http.Request, projectID int64) {
	switch r.Method {
	case http.MethodGet:
		value, err := h.store.ProjectProfile(r.Context(), projectID)
		if h.writeError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, value)
	case http.MethodPut:
		var value orchestration.ProjectProfile
		if err := decodeJSON(r, &value); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if value.ProjectID != 0 && value.ProjectID != projectID {
			writeError(w, http.StatusConflict, fmt.Errorf("project id mismatch"))
			return
		}
		updated, err := h.store.UpdateProjectProfile(r.Context(), orchestration.ProjectProfileInput{
			ProjectID: projectID, ResourceProfile: value.ResourceProfile, Methodology: value.Methodology,
			ResultProtocol: value.ResultProtocol, DeliveryMode: value.DeliveryMode, QASessionMode: value.QASessionMode,
			AutoMerge: value.AutoMerge, AutonomyMode: value.AutonomyMode, AutonomyState: value.AutonomyState,
			ControlIssueNumber: value.ControlIssueNumber, MaxActiveWorkUnits: value.MaxActiveWorkUnits,
			MaxParallelImplementors: value.MaxParallelImplementors, MaxParallelQA: value.MaxParallelQA,
			ChatGPTProjectURL: value.ChatGPTProjectURL,
		})
		if h.writeError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

func (h *provisioningHandler) writeError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, orchestration.ErrNotFound), errors.Is(err, browserbinding.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, orchestration.ErrConflict), errors.Is(err, browserbinding.ErrConflict):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
	return true
}
