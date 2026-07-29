package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	deliveryModeReviewed = "reviewed"
	deliveryModeAuto     = "auto"
	qaModeManualFresh    = "manual_fresh_binding"
)

func (s *Store) ProjectProfile(ctx context.Context, projectID int64) (ProjectProfile, error) {
	if projectID <= 0 {
		return ProjectProfile{}, fmt.Errorf("project id must be positive")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id=?`, projectID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectProfile{}, ErrNotFound
		}
		return ProjectProfile{}, fmt.Errorf("read Project: %w", err)
	}
	now := stamp(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_execution_profiles (
		project_id,resource_profile,methodology,result_protocol,delivery_mode,qa_session_mode,auto_merge,updated_at,
		autonomy_mode,autonomy_state,control_issue_number,max_active_work_units,max_parallel_implementors,max_parallel_qa
	) VALUES (?,?,?,?,?,?,0,?,?,?,?,?,?,?) ON CONFLICT(project_id) DO NOTHING`,
		projectID, ManualResourceProfile, ManualMethodology, ManualResultProtocol, deliveryModeReviewed, qaModeManualFresh,
		now, AutonomyModeManual, AutonomyStateDisabled, 0, 3, 3, 3)
	if err != nil {
		return ProjectProfile{}, fmt.Errorf("ensure Project execution profile: %w", err)
	}
	return s.readProjectProfile(ctx, projectID)
}

func (s *Store) UpdateProjectProfile(ctx context.Context, input ProjectProfileInput) (ProjectProfile, error) {
	input = normalizeProjectProfileInput(input)
	if err := validateProjectProfileInput(input); err != nil {
		return ProjectProfile{}, err
	}
	if _, err := s.ProjectProfile(ctx, input.ProjectID); err != nil {
		return ProjectProfile{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE project_execution_profiles SET
		resource_profile=?,methodology=?,result_protocol=?,delivery_mode=?,qa_session_mode=?,auto_merge=0,
		autonomy_mode=?,autonomy_state=?,control_issue_number=?,max_active_work_units=?,max_parallel_implementors=?,max_parallel_qa=?,updated_at=?
		WHERE project_id=?`,
		input.ResourceProfile, input.Methodology, input.ResultProtocol, input.DeliveryMode, input.QASessionMode,
		input.AutonomyMode, input.AutonomyState, input.ControlIssueNumber, input.MaxActiveWorkUnits,
		input.MaxParallelImplementors, input.MaxParallelQA, stamp(s.now()), input.ProjectID)
	if err != nil {
		return ProjectProfile{}, fmt.Errorf("update Project autonomy profile: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return ProjectProfile{}, ErrNotFound
	}
	return s.readProjectProfile(ctx, input.ProjectID)
}

func (s *Store) readProjectProfile(ctx context.Context, projectID int64) (ProjectProfile, error) {
	var value ProjectProfile
	var autoMerge int
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT
		project_id,resource_profile,methodology,result_protocol,delivery_mode,qa_session_mode,auto_merge,
		autonomy_mode,autonomy_state,control_issue_number,max_active_work_units,max_parallel_implementors,max_parallel_qa,updated_at
		FROM project_execution_profiles WHERE project_id=?`, projectID).Scan(
		&value.ProjectID, &value.ResourceProfile, &value.Methodology, &value.ResultProtocol, &value.DeliveryMode,
		&value.QASessionMode, &autoMerge, &value.AutonomyMode, &value.AutonomyState, &value.ControlIssueNumber,
		&value.MaxActiveWorkUnits, &value.MaxParallelImplementors, &value.MaxParallelQA, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectProfile{}, ErrNotFound
		}
		return ProjectProfile{}, fmt.Errorf("read Project autonomy profile: %w", err)
	}
	value.AutoMerge = autoMerge != 0
	value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return ProjectProfile{}, fmt.Errorf("parse Project autonomy profile timestamp: %w", err)
	}
	return value, nil
}

func normalizeProjectProfileInput(input ProjectProfileInput) ProjectProfileInput {
	input.ResourceProfile = strings.TrimSpace(input.ResourceProfile)
	input.Methodology = strings.TrimSpace(input.Methodology)
	input.ResultProtocol = strings.TrimSpace(input.ResultProtocol)
	input.DeliveryMode = strings.TrimSpace(input.DeliveryMode)
	input.QASessionMode = strings.TrimSpace(input.QASessionMode)
	input.AutonomyMode = strings.TrimSpace(input.AutonomyMode)
	input.AutonomyState = strings.TrimSpace(input.AutonomyState)
	if input.AutonomyMode == "" {
		input.AutonomyMode = AutonomyModeManual
	}
	if input.AutonomyState == "" {
		input.AutonomyState = AutonomyStateDisabled
	}
	if input.DeliveryMode == "" {
		input.DeliveryMode = deliveryModeReviewed
	}
	if input.QASessionMode == "" {
		input.QASessionMode = qaModeManualFresh
	}
	if input.MaxActiveWorkUnits == 0 {
		input.MaxActiveWorkUnits = 3
	}
	if input.MaxParallelImplementors == 0 {
		input.MaxParallelImplementors = 3
	}
	if input.MaxParallelQA == 0 {
		input.MaxParallelQA = 3
	}
	if input.AutonomyMode == AutonomyModeContinuous {
		if input.ResourceProfile == "" {
			input.ResourceProfile = ContinuousResourceProfile
		}
		if input.Methodology == "" {
			input.Methodology = ContinuousMethodology
		}
		if input.ResultProtocol == "" {
			input.ResultProtocol = ContinuousResultProtocol
		}
	} else {
		if input.ResourceProfile == "" {
			input.ResourceProfile = ManualResourceProfile
		}
		if input.Methodology == "" {
			input.Methodology = ManualMethodology
		}
		if input.ResultProtocol == "" {
			input.ResultProtocol = ManualResultProtocol
		}
	}
	return input
}

func validateProjectProfileInput(input ProjectProfileInput) error {
	if input.ProjectID <= 0 || input.AutoMerge {
		return fmt.Errorf("project id must be positive and auto_merge must remain false")
	}
	if input.DeliveryMode != deliveryModeReviewed && input.DeliveryMode != deliveryModeAuto {
		return fmt.Errorf("delivery mode must be reviewed or auto")
	}
	if input.QASessionMode != qaModeManualFresh {
		return fmt.Errorf("qa_session_mode must remain manual_fresh_binding until M11")
	}
	if input.MaxActiveWorkUnits < 1 || input.MaxActiveWorkUnits > 64 || input.MaxParallelImplementors < 1 || input.MaxParallelImplementors > input.MaxActiveWorkUnits || input.MaxParallelQA < 1 || input.MaxParallelQA > input.MaxActiveWorkUnits {
		return fmt.Errorf("invalid Project WIP limits")
	}
	if !validAutonomyState(input.AutonomyState) {
		return fmt.Errorf("unsupported autonomy state %q", input.AutonomyState)
	}
	switch input.AutonomyMode {
	case AutonomyModeManual:
		if input.ResourceProfile != ManualResourceProfile || input.Methodology != ManualMethodology || input.ResultProtocol != ManualResultProtocol || input.AutonomyState != AutonomyStateDisabled || input.ControlIssueNumber != 0 {
			return fmt.Errorf("manual_owner_dispatch requires the v1 profile, disabled state and no Control Issue")
		}
	case AutonomyModeContinuous:
		if input.ResourceProfile != ContinuousResourceProfile || input.Methodology != ContinuousMethodology || input.ResultProtocol != ContinuousResultProtocol || input.ControlIssueNumber <= 0 {
			return fmt.Errorf("continuous orchestration requires the exact v2 profile and a positive Control Issue")
		}
	default:
		return fmt.Errorf("unsupported autonomy mode %q", input.AutonomyMode)
	}
	return nil
}

func validAutonomyState(value string) bool {
	switch value {
	case AutonomyStateDisabled, AutonomyStateEnabled, AutonomyStatePaused, AutonomyStateStopped:
		return true
	default:
		return false
	}
}
