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

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

type ProjectSyncer interface {
	SyncProject(ctx context.Context, projectID int64) (supervisor.ProjectSnapshot, error)
}

type Server struct {
	db                         *sql.DB
	store                      *supervisor.Store
	syncer                     ProjectSyncer
	defaultPollIntervalSeconds int64
}

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

type createProjectRequest struct {
	Owner               string `json:"owner"`
	Repository          string `json:"repository"`
	WorkflowMode        string `json:"workflow_mode"`
	PollingEnabled      *bool  `json:"polling_enabled"`
	PollIntervalSeconds *int64 `json:"poll_interval_seconds"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func New(db *sql.DB, store *supervisor.Store, syncer ProjectSyncer, defaultPollInterval time.Duration) http.Handler {
	seconds := int64(defaultPollInterval / time.Second)
	if seconds <= 0 {
		seconds = 300
	}
	server := &Server{
		db:                         db,
		store:                      store,
		syncer:                     syncer,
		defaultPollIntervalSeconds: seconds,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", server.health)
	mux.HandleFunc("/api/projects", server.projects)
	mux.HandleFunc("/api/projects/", server.project)
	mux.HandleFunc("/api/workspace", server.workspace)
	mux.HandleFunc("/api/workspace/state", server.workspaceState)
	mux.HandleFunc("/api/attention", server.attention)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	response := healthResponse{Status: "healthy", Database: "connected"}
	statusCode := http.StatusOK
	if err := s.db.PingContext(ctx); err != nil {
		response.Status = "unhealthy"
		response.Database = "unavailable"
		statusCode = http.StatusServiceUnavailable
	}

	writeJSON(w, statusCode, response)
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projects, err := s.store.ListProjects(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
	case http.MethodPost:
		var request createProjectRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		workflowMode := strings.TrimSpace(request.WorkflowMode)
		if workflowMode == "" {
			workflowMode = "pull_request"
		}
		pollingEnabled := true
		if request.PollingEnabled != nil {
			pollingEnabled = *request.PollingEnabled
		}
		pollInterval := s.defaultPollIntervalSeconds
		if request.PollIntervalSeconds != nil {
			pollInterval = *request.PollIntervalSeconds
		}
		project, err := s.store.CreateProject(r.Context(), supervisor.CreateProjectInput{
			Owner: request.Owner, Repository: request.Repository, WorkflowMode: workflowMode,
			PollingEnabled: pollingEnabled, PollIntervalSeconds: pollInterval,
		})
		if errors.Is(err, supervisor.ErrConflict) {
			writeError(w, http.StatusConflict, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, project)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) project(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
	parts := strings.Split(path, "/")
	if path == "" || len(parts) > 4 {
		http.NotFound(w, r)
		return
	}
	projectID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || projectID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid project id"))
		return
	}

	switch {
	case len(parts) == 2 && parts[1] == "sync":
		s.manualSync(w, r, projectID)
		return
	case len(parts) == 2 && parts[1] == "state":
		s.projectState(w, r, projectID)
		return
	case len(parts) == 2 && parts[1] == "attention":
		s.projectAttention(w, r, projectID)
		return
	case len(parts) == 4 && parts[1] == "work-units" && parts[3] == "state":
		issueNumber, err := strconv.Atoi(parts[2])
		if err != nil || issueNumber <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid issue number"))
			return
		}
		s.workUnitState(w, r, projectID, issueNumber)
		return
	case len(parts) != 1:
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		snapshot, err := s.store.ProjectSnapshot(r.Context(), projectID)
		if errors.Is(err, supervisor.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case http.MethodDelete:
		if err := s.store.DeleteProject(r.Context(), projectID); errors.Is(err, supervisor.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *Server) manualSync(w http.ResponseWriter, r *http.Request, projectID int64) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.syncer == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("synchronization is unavailable"))
		return
	}
	snapshot, err := s.syncer.SyncProject(r.Context(), projectID)
	if errors.Is(err, supervisor.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	workspace, err := s.store.WorkspaceSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s *Server) workspaceState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	workspace, err := s.store.WorkspaceSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, workflow.DeriveWorkspace(workspace))
}

func (s *Server) attention(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	workspace, err := s.store.WorkspaceSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	state := workflow.DeriveWorkspace(workspace)
	writeJSON(w, http.StatusOK, map[string]any{"attention": state.Attention})
}

func (s *Server) projectState(w http.ResponseWriter, r *http.Request, projectID int64) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	state, ok := s.readProjectState(w, r, projectID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) projectAttention(w http.ResponseWriter, r *http.Request, projectID int64) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	state, ok := s.readProjectState(w, r, projectID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": state.Project, "attention": state.Attention})
}

func (s *Server) workUnitState(w http.ResponseWriter, r *http.Request, projectID int64, issueNumber int) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	state, ok := s.readProjectState(w, r, projectID)
	if !ok {
		return
	}
	workUnit, found := workflow.FindWorkUnit(state, issueNumber)
	if !found {
		writeError(w, http.StatusNotFound, fmt.Errorf("work unit not found"))
		return
	}
	writeJSON(w, http.StatusOK, workUnit)
}

func (s *Server) readProjectState(w http.ResponseWriter, r *http.Request, projectID int64) (workflow.ProjectState, bool) {
	snapshot, err := s.store.ProjectSnapshot(r.Context(), projectID)
	if errors.Is(err, supervisor.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return workflow.ProjectState{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return workflow.ProjectState{}, false
	}
	return workflow.DeriveProject(snapshot), true
}

func decodeJSON(r *http.Request, destination any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorResponse{Error: err.Error()})
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}
