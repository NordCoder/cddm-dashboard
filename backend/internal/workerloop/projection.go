package workerloop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

const (
	DefaultMethodology       = "cddm-minimal/v2.0"
	DefaultResultProtocol    = "cddm-worker-result/v1"
	DeliveryModeReviewed     = "reviewed"
	DeliveryModeAuto         = "auto"
	QAModeManualFresh        = "manual_fresh_binding"
	ChatCreationModeManual   = "manual"
	ChatCreationModeAutomatic = "automatic"
)

type PlannerHealthProvider interface {
	Health(context.Context) planning.Health
}

type ExecutionProfile struct {
	ProjectID        int64     `json:"project_id"`
	ResourceProfile  string    `json:"resource_version"`
	Methodology      string    `json:"methodology_version"`
	ResultProtocol   string    `json:"result_protocol"`
	DeliveryMode     string    `json:"delivery_mode"`
	QASessionMode    string    `json:"qa_session_mode"`
	ChatCreationMode string    `json:"chat_creation_mode"`
	AutoMerge        bool      `json:"auto_merge"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type DeliveryEvidence struct {
	CommandID      string `json:"command_id"`
	Status         string `json:"status"`
	BindingID      string `json:"binding_id"`
	BindingVersion int64  `json:"binding_version"`
	WorkerID       string `json:"worker_id"`
	TargetRole     string `json:"target_role"`
	LaneKey        string `json:"lane_key"`
	OutcomeReason  string `json:"outcome_reason,omitempty"`
}

type ResultEvidenceView struct {
	GitHubCommentID  int64      `json:"github_comment_id"`
	Role             string     `json:"role"`
	Result           string     `json:"result"`
	ValidationStatus string     `json:"validation_status"`
	ValidationReason string     `json:"validation_reason,omitempty"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
}

type RoleBinding struct {
	Role    string                  `json:"role"`
	LaneKey string                  `json:"lane_key"`
	Binding *browserbinding.Binding `json:"binding,omitempty"`
}

type WorkUnitExecution struct {
	ProjectID        int64               `json:"project_id"`
	IssueNumber      int                 `json:"issue_number"`
	Profile          ExecutionProfile    `json:"profile"`
	ActiveCommand    *Command            `json:"active_workflow_command,omitempty"`
	Delivery         *DeliveryEvidence   `json:"delivery,omitempty"`
	DeliveryStatus   string              `json:"delivery_status"`
	ExecutionStatus  string              `json:"execution_status"`
	WorkerResult     *ResultEvidenceView `json:"worker_result,omitempty"`
	ValidationStatus string              `json:"validation_status"`
	RoleBindings     []RoleBinding       `json:"role_bindings"`
	NextAction       string              `json:"next_action"`
}

type ReadinessCheck struct {
	Code   string `json:"code"`
	Ready  bool   `json:"ready"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type PilotReadiness struct {
	ProjectID      int64              `json:"project_id"`
	IssueNumber    int                `json:"issue_number"`
	Ready          bool               `json:"ready"`
	Status         string             `json:"status"`
	ResourceDigest string             `json:"resource_digest"`
	Profile        ExecutionProfile   `json:"profile"`
	Checks         []ReadinessCheck   `json:"checks"`
	Warnings       []workflow.Warning `json:"protocol_warnings"`
}

type ProjectionService struct {
	db        *sql.DB
	projects  *supervisor.Store
	commands  *Store
	bindings  *browserbinding.Service
	resources resourcepack.Package
	planner   PlannerHealthProvider
}

func NewProjectionService(db *sql.DB, projects *supervisor.Store, commands *Store, bindings *browserbinding.Service, resources resourcepack.Package, planner PlannerHealthProvider) *ProjectionService {
	return &ProjectionService{db: db, projects: projects, commands: commands, bindings: bindings, resources: resources, planner: planner}
}

func (s *ProjectionService) Profile(ctx context.Context, projectID int64) (ExecutionProfile, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return ExecutionProfile{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_execution_profiles (project_id,resource_profile,methodology,result_protocol,delivery_mode,qa_session_mode,chat_creation_mode,auto_merge,updated_at) VALUES (?,?,?,?,?,?,?,0,?) ON CONFLICT(project_id) DO NOTHING`, projectID, resourcepack.DefaultProfile, DefaultMethodology, DefaultResultProtocol, DeliveryModeReviewed, QAModeManualFresh, ChatCreationModeManual, now)
	if err != nil {
		return ExecutionProfile{}, err
	}
	return s.readProfile(ctx, projectID)
}

func (s *ProjectionService) UpdateProfile(ctx context.Context, profile ExecutionProfile) (ExecutionProfile, error) {
	profile.ResourceProfile = strings.TrimSpace(profile.ResourceProfile)
	profile.Methodology = strings.TrimSpace(profile.Methodology)
	profile.ResultProtocol = strings.TrimSpace(profile.ResultProtocol)
	profile.DeliveryMode = strings.TrimSpace(profile.DeliveryMode)
	profile.QASessionMode = strings.TrimSpace(profile.QASessionMode)
	profile.ChatCreationMode = strings.TrimSpace(profile.ChatCreationMode)
	if profile.ProjectID <= 0 || profile.ResourceProfile != resourcepack.DefaultProfile || profile.Methodology != DefaultMethodology || profile.ResultProtocol != DefaultResultProtocol {
		return ExecutionProfile{}, fmt.Errorf("unsupported execution profile identity")
	}
	if profile.DeliveryMode != DeliveryModeReviewed && profile.DeliveryMode != DeliveryModeAuto {
		return ExecutionProfile{}, fmt.Errorf("delivery mode must be reviewed or auto")
	}
	if profile.ChatCreationMode != ChatCreationModeManual && profile.ChatCreationMode != ChatCreationModeAutomatic {
		return ExecutionProfile{}, fmt.Errorf("chat_creation_mode must be manual or automatic")
	}
	if profile.QASessionMode != QAModeManualFresh || profile.AutoMerge {
		return ExecutionProfile{}, fmt.Errorf("qa_session_mode must be manual_fresh_binding and auto_merge must remain false")
	}
	if _, err := s.Profile(ctx, profile.ProjectID); err != nil {
		return ExecutionProfile{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE project_execution_profiles SET resource_profile=?,methodology=?,result_protocol=?,delivery_mode=?,qa_session_mode=?,chat_creation_mode=?,auto_merge=0,updated_at=? WHERE project_id=?`, profile.ResourceProfile, profile.Methodology, profile.ResultProtocol, profile.DeliveryMode, profile.QASessionMode, profile.ChatCreationMode, now, profile.ProjectID)
	if err != nil {
		return ExecutionProfile{}, err
	}
	return s.readProfile(ctx, profile.ProjectID)
}

func (s *ProjectionService) WorkUnit(ctx context.Context, projectID int64, issueNumber int) (WorkUnitExecution, error) {
	project, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return WorkUnitExecution{}, err
	}
	profile, err := s.Profile(ctx, projectID)
	if err != nil {
		return WorkUnitExecution{}, err
	}
	commands, err := s.commands.ListCommands(ctx, projectID, issueNumber)
	if err != nil {
		return WorkUnitExecution{}, err
	}
	current := currentProjectionCommand(commands)
	projection := WorkUnitExecution{
		ProjectID: projectID, IssueNumber: issueNumber, Profile: profile,
		DeliveryStatus: "not_created", ExecutionStatus: "not_created", ValidationStatus: "not_observed",
		RoleBindings: make([]RoleBinding, 0, 3), NextAction: "derive_current_route",
	}
	for _, role := range []string{"lead", "implementor", "qa"} {
		lane := LogicalLane(project, issueNumber, role)
		binding, readErr := s.bindings.Get(ctx, projectID, lane)
		if errors.Is(readErr, browserbinding.ErrNotFound) {
			projection.RoleBindings = append(projection.RoleBindings, RoleBinding{Role: role, LaneKey: lane})
			continue
		}
		if readErr != nil {
			return WorkUnitExecution{}, readErr
		}
		projection.RoleBindings = append(projection.RoleBindings, RoleBinding{Role: role, LaneKey: lane, Binding: &binding})
	}
	if current == nil {
		return projection, nil
	}
	projection.ActiveCommand = current
	projection.ExecutionStatus = current.Status
	projection.NextAction = nextActionForCommand(current.Status)
	if delivery, ok, readErr := s.deliveryForCommand(ctx, current.ID); readErr != nil {
		return WorkUnitExecution{}, readErr
	} else if ok {
		projection.Delivery = &delivery
		projection.DeliveryStatus = delivery.Status
	}
	results, err := s.commands.ResultsForCommand(ctx, projectID, current.ID)
	if err != nil {
		return WorkUnitExecution{}, err
	}
	if len(results) > 0 {
		result := results[len(results)-1]
		projection.WorkerResult = &ResultEvidenceView{
			GitHubCommentID: result.GitHubCommentID, Role: result.Role, Result: result.Result,
			ValidationStatus: result.ValidationStatus, ValidationReason: result.ValidationReason, AcceptedAt: result.AcceptedAt,
		}
		projection.ValidationStatus = result.ValidationStatus
	}
	return projection, nil
}

func (s *ProjectionService) Readiness(ctx context.Context, projectID int64, issueNumber int) (PilotReadiness, error) {
	snapshot, err := s.projects.ProjectSnapshot(ctx, projectID)
	if err != nil {
		return PilotReadiness{}, err
	}
	state := workflow.DeriveProject(snapshot)
	workUnit, ok := workflow.FindWorkUnit(state, issueNumber)
	if !ok {
		return PilotReadiness{}, fmt.Errorf("work unit not found")
	}
	execution, err := s.WorkUnit(ctx, projectID, issueNumber)
	if err != nil {
		return PilotReadiness{}, err
	}
	plannerHealth := s.planner.Health(ctx)
	checks := []ReadinessCheck{
		check("github_synchronization", snapshot.Project.SyncStatus == "healthy", snapshot.Project.SyncStatus, snapshot.Project.SyncError),
		check("resource_package", execution.Profile.ResourceProfile == s.resources.Profile && s.resources.Digest != "", execution.Profile.ResourceProfile, s.resources.Digest),
		check("prompt_planner", plannerHealth.Status == "healthy" || plannerHealth.Status == "disabled", plannerHealth.Status, "deterministic fallback remains available when OpenCode is disabled"),
		check("browser_worker", hasLiveWorker(execution.RoleBindings), bindingStatus(execution.RoleBindings), "at least one bound browser worker must be live"),
		check("lead_lane", roleReady(execution.RoleBindings, "lead"), roleStatus(execution.RoleBindings, "lead"), "bind the Lead chat for this Work Unit"),
		check("implementor_lane", roleReady(execution.RoleBindings, "implementor"), roleStatus(execution.RoleBindings, "implementor"), "bind the Implementor chat for this Work Unit"),
		check("qa_mode", execution.Profile.QASessionMode == QAModeManualFresh, execution.Profile.QASessionMode, "fresh QA chat is bound only when a QA command is ready"),
		check("ci_observable", workUnit.Candidate.Current == nil || workUnit.CI.Source != "", workUnit.CI.Status, workUnit.CI.Source),
		check("marker_parser", true, "enabled", DefaultResultProtocol),
		check("protocol_errors", len(workUnit.Warnings) == 0, fmt.Sprintf("%d warnings", len(workUnit.Warnings)), "resolve protocol warnings before pilot execution"),
		check("auto_merge", !execution.Profile.AutoMerge, "disabled", "merge requires explicit Lead authority"),
	}
	ready := true
	for _, item := range checks {
		ready = ready && item.Ready
	}
	status := "not_ready"
	if ready {
		status = "pilot_ready"
	}
	return PilotReadiness{
		ProjectID: projectID, IssueNumber: issueNumber, Ready: ready, Status: status,
		ResourceDigest: s.resources.Digest, Profile: execution.Profile, Checks: checks, Warnings: workUnit.Warnings,
	}, nil
}

func LogicalLane(project supervisor.Project, issueNumber int, role string) string {
	return fmt.Sprintf("%s/%s#%d:%s", strings.ToLower(project.Owner), strings.ToLower(project.Repository), issueNumber, strings.ToLower(strings.TrimSpace(role)))
}

func ValidRole(role string) bool {
	return role == "lead" || role == "implementor" || role == "qa"
}

func (s *ProjectionService) readProfile(ctx context.Context, projectID int64) (ExecutionProfile, error) {
	var value ExecutionProfile
	var autoMerge int
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT project_id,resource_profile,methodology,result_protocol,delivery_mode,qa_session_mode,chat_creation_mode,auto_merge,updated_at FROM project_execution_profiles WHERE project_id=?`, projectID).Scan(&value.ProjectID, &value.ResourceProfile, &value.Methodology, &value.ResultProtocol, &value.DeliveryMode, &value.QASessionMode, &value.ChatCreationMode, &autoMerge, &updated)
	if err != nil {
		return ExecutionProfile{}, err
	}
	value.AutoMerge = autoMerge != 0
	value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return value, err
}

func currentProjectionCommand(commands []Command) *Command {
	var latest *Command
	for index := range commands {
		command := commands[index]
		if latest == nil || activeCommandStatus(command.Status) || !activeCommandStatus(latest.Status) {
			copy := command
			latest = &copy
		}
	}
	return latest
}

func (s *ProjectionService) deliveryForCommand(ctx context.Context, commandID string) (DeliveryEvidence, bool, error) {
	var value DeliveryEvidence
	err := s.db.QueryRowContext(ctx, `SELECT d.id,d.status,d.binding_id,d.binding_version,d.worker_id,d.target_role,d.lane_key,d.outcome_reason FROM workflow_delivery_links l JOIN delivery_commands d ON d.id=l.delivery_command_id WHERE l.workflow_command_id=?`, commandID).Scan(&value.CommandID, &value.Status, &value.BindingID, &value.BindingVersion, &value.WorkerID, &value.TargetRole, &value.LaneKey, &value.OutcomeReason)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryEvidence{}, false, nil
	}
	return value, err == nil, err
}

func nextActionForCommand(status string) string {
	switch status {
	case CommandCreated, CommandDeliveryPending:
		return "deliver_prompt"
	case CommandAwaitingResult:
		return "await_github_worker_result"
	case CommandAmbiguous, CommandFailed:
		return "lead_manual_attention"
	default:
		return "derive_current_route"
	}
}

func check(code string, ready bool, status, detail string) ReadinessCheck {
	return ReadinessCheck{Code: code, Ready: ready, Status: status, Detail: detail}
}

func roleReady(bindings []RoleBinding, role string) bool {
	for _, item := range bindings {
		if item.Role == role {
			return item.Binding != nil && item.Binding.Enabled && item.Binding.Readiness == "ready"
		}
	}
	return false
}

func roleStatus(bindings []RoleBinding, role string) string {
	for _, item := range bindings {
		if item.Role == role {
			if item.Binding == nil {
				return "unbound"
			}
			return item.Binding.Readiness
		}
	}
	return "unbound"
}

func hasLiveWorker(bindings []RoleBinding) bool {
	for _, item := range bindings {
		if item.Binding != nil && item.Binding.Readiness == "ready" {
			return true
		}
	}
	return false
}

func bindingStatus(bindings []RoleBinding) string {
	if hasLiveWorker(bindings) {
		return "connected"
	}
	return "unavailable"
}
