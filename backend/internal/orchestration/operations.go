package orchestration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type OperationsService struct {
	store     *Store
	scheduler *Scheduler
}

func NewOperationsService(store *Store) *OperationsService {
	return &OperationsService{store: store, scheduler: NewScheduler(store)}
}

func (s *OperationsService) Enable(ctx context.Context, projectID, expectedRevision int64) (AutopilotStatus, error) {
	return s.mutateControl(ctx, projectID, expectedRevision, "enable")
}

func (s *OperationsService) Pause(ctx context.Context, projectID, expectedRevision int64) (AutopilotStatus, error) {
	return s.mutateControl(ctx, projectID, expectedRevision, "pause")
}

func (s *OperationsService) Resume(ctx context.Context, projectID, expectedRevision int64) (AutopilotStatus, error) {
	return s.mutateControl(ctx, projectID, expectedRevision, "resume")
}

func (s *OperationsService) Stop(ctx context.Context, projectID, expectedRevision int64) (AutopilotStatus, error) {
	return s.mutateControl(ctx, projectID, expectedRevision, "stop")
}

func (s *OperationsService) mutateControl(ctx context.Context, projectID, expectedRevision int64, action string) (AutopilotStatus, error) {
	if projectID <= 0 || expectedRevision < 0 {
		return AutopilotStatus{}, fmt.Errorf("invalid Autopilot control identity")
	}
	if _, err := s.store.ProjectProfile(ctx, projectID); err != nil {
		return AutopilotStatus{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AutopilotStatus{}, fmt.Errorf("begin Autopilot control mutation: %w", err)
	}
	defer tx.Rollback()

	profile, err := projectProfileTx(ctx, tx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	control, err := ensureControlTx(ctx, tx, projectID, s.store.now())
	if err != nil {
		return AutopilotStatus{}, err
	}
	target, err := controlTarget(action, profile.AutonomyState)
	if err != nil {
		return AutopilotStatus{}, err
	}
	if control.Revision == expectedRevision+1 && control.LastAction == action && profile.AutonomyState == target {
		if err := tx.Commit(); err != nil {
			return AutopilotStatus{}, err
		}
		return s.Status(ctx, projectID)
	}
	if control.Revision != expectedRevision {
		return AutopilotStatus{}, ErrConflict
	}
	if action == "enable" || action == "resume" {
		if err := enablePrerequisitesTx(ctx, tx, profile); err != nil {
			return AutopilotStatus{}, err
		}
	}

	now := s.store.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE project_execution_profiles SET autonomy_state=?,updated_at=? WHERE project_id=? AND autonomy_state=?`, target, stamp(now), projectID, profile.AutonomyState)
	if err != nil {
		return AutopilotStatus{}, fmt.Errorf("update Autopilot state: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return AutopilotStatus{}, ErrConflict
	}
	if action == "stop" {
		if err := supersedeSafePendingTx(ctx, tx, projectID, now); err != nil {
			return AutopilotStatus{}, err
		}
	}
	if err := advanceControlTx(ctx, tx, projectID, expectedRevision, action, now); err != nil {
		return AutopilotStatus{}, err
	}
	if err := tx.Commit(); err != nil {
		return AutopilotStatus{}, fmt.Errorf("commit Autopilot control mutation: %w", err)
	}
	return s.Status(ctx, projectID)
}

func controlTarget(action, current string) (string, error) {
	switch action {
	case "enable":
		if current != AutonomyStateDisabled && current != AutonomyStateStopped && current != AutonomyStateEnabled {
			return "", fmt.Errorf("enable requires disabled or stopped Autopilot state")
		}
		return AutonomyStateEnabled, nil
	case "pause":
		if current != AutonomyStateEnabled && current != AutonomyStatePaused {
			return "", fmt.Errorf("pause requires enabled Autopilot state")
		}
		return AutonomyStatePaused, nil
	case "resume":
		if current != AutonomyStatePaused && current != AutonomyStateEnabled {
			return "", fmt.Errorf("resume requires paused Autopilot state")
		}
		return AutonomyStateEnabled, nil
	case "stop":
		if current != AutonomyStateEnabled && current != AutonomyStatePaused && current != AutonomyStateDisabled && current != AutonomyStateStopped {
			return "", fmt.Errorf("unsupported Autopilot state %q", current)
		}
		return AutonomyStateStopped, nil
	default:
		return "", fmt.Errorf("unsupported Autopilot control %q", action)
	}
}

func enablePrerequisitesTx(ctx context.Context, tx *sql.Tx, profile ProjectProfile) error {
	if profile.AutonomyMode != AutonomyModeContinuous || profile.ResourceProfile != ContinuousResourceProfile || profile.Methodology != ContinuousMethodology || profile.ResultProtocol != ContinuousResultProtocol {
		return fmt.Errorf("Autopilot requires the exact continuous v2 profile")
	}
	if profile.ControlIssueNumber <= 0 {
		return fmt.Errorf("Autopilot requires a positive Project Control Issue")
	}
	if strings.TrimSpace(profile.ChatGPTProjectURL) == "" {
		return fmt.Errorf("Autopilot requires an exact ChatGPT Project URL")
	}
	var syncStatus, syncError, completed string
	if err := tx.QueryRowContext(ctx, `SELECT sync_status,sync_error,COALESCE(last_sync_completed_at,'') FROM projects WHERE id=?`, profile.ProjectID).Scan(&syncStatus, &syncError, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read GitHub synchronization state: %w", err)
	}
	if syncStatus != "healthy" || completed == "" {
		if syncError == "" {
			syncError = syncStatus
		}
		return fmt.Errorf("GitHub synchronization is not healthy: %s", syncError)
	}
	var blockers int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM autopilot_circuit_breakers WHERE project_id=? AND status IN ('open','acknowledged')`, profile.ProjectID).Scan(&blockers); err != nil {
		return fmt.Errorf("read active circuit breakers: %w", err)
	}
	if blockers > 0 {
		return fmt.Errorf("Autopilot has unresolved circuit breakers")
	}
	return nil
}

func supersedeSafePendingTx(ctx context.Context, tx *sql.Tx, projectID int64, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE session_provision_requests SET status='superseded',completion_reason='autopilot_stopped',updated_at=?,completed_at=? WHERE project_id=? AND status='pending'`, stamp(now), stamp(now), projectID); err != nil {
		return fmt.Errorf("supersede pending provisioning: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='superseded',reason_code='autopilot_stopped',updated_at=?,completed_at=? WHERE project_id=? AND status IN ('pending','blocked') AND workflow_command_id='' AND delivery_command_id=''`, stamp(now), stamp(now), projectID); err != nil {
		return fmt.Errorf("supersede safe command materializations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_intents SET status='superseded',updated_at=? WHERE project_id=? AND status='pending' AND NOT EXISTS (
		SELECT 1 FROM autonomous_command_materializations m
		WHERE m.project_id=workflow_intents.project_id AND m.intent_id=workflow_intents.id
		  AND (m.workflow_command_id<>'' OR m.delivery_command_id<>'' OR m.status IN ('materialized','completed','ambiguous'))
	)`, stamp(now), projectID); err != nil {
		return fmt.Errorf("supersede safe pending Intents: %w", err)
	}
	return nil
}

func (s *OperationsService) TripBreaker(ctx context.Context, input BreakerTripInput) (AutopilotStatus, error) {
	input.ScopeKind = strings.TrimSpace(input.ScopeKind)
	if input.ScopeKind == "" {
		input.ScopeKind = BreakerScopeProject
	}
	input.LaneKey = strings.TrimSpace(input.LaneKey)
	input.Code = strings.TrimSpace(input.Code)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Evidence = strings.TrimSpace(input.Evidence)
	input.ExpectedHead = strings.TrimSpace(input.ExpectedHead)
	if err := validateBreakerTrip(input); err != nil {
		return AutopilotStatus{}, err
	}
	if _, err := s.store.ProjectProfile(ctx, input.ProjectID); err != nil {
		return AutopilotStatus{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AutopilotStatus{}, err
	}
	defer tx.Rollback()
	if _, err := projectProfileTx(ctx, tx, input.ProjectID); err != nil {
		return AutopilotStatus{}, err
	}
	control, err := ensureControlTx(ctx, tx, input.ProjectID, s.store.now())
	if err != nil {
		return AutopilotStatus{}, err
	}
	lastAction := "trip:" + input.Code + ":" + input.ScopeKind + ":" + input.LaneKey
	if control.Revision == input.ExpectedRevision+1 && control.LastAction == lastAction {
		if err := tx.Commit(); err != nil {
			return AutopilotStatus{}, err
		}
		return s.Status(ctx, input.ProjectID)
	}
	if control.Revision != input.ExpectedRevision {
		return AutopilotStatus{}, ErrConflict
	}
	now := s.store.now().UTC()
	var existingID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM autopilot_circuit_breakers WHERE project_id=? AND scope_kind=? AND lane_key=? AND code=? AND status IN ('open','acknowledged')`, input.ProjectID, input.ScopeKind, input.LaneKey, input.Code).Scan(&existingID)
	switch {
	case err == nil:
		_, err = tx.ExecContext(ctx, `UPDATE autopilot_circuit_breakers SET reason=?,recovery_requirements=?,evidence=?,expected_head=?,status='open',occurrence_count=occurrence_count+1,updated_at=?,acknowledged_at='',resolved_at='' WHERE id=?`, input.Reason, breakerRecovery(input.Code), input.Evidence, input.ExpectedHead, stamp(now), existingID)
	case errors.Is(err, sql.ErrNoRows):
		id := deterministicBreakerID(input.ProjectID, input.ScopeKind, input.LaneKey, input.Code, control.Revision+1)
		_, err = tx.ExecContext(ctx, `INSERT INTO autopilot_circuit_breakers(id,project_id,scope_kind,lane_key,code,reason,recovery_requirements,evidence,expected_head,status,occurrence_count,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'open',1,?,?)`, id, input.ProjectID, input.ScopeKind, input.LaneKey, input.Code, input.Reason, breakerRecovery(input.Code), input.Evidence, input.ExpectedHead, stamp(now), stamp(now))
	default:
		return AutopilotStatus{}, fmt.Errorf("read active circuit breaker: %w", err)
	}
	if err != nil {
		return AutopilotStatus{}, fmt.Errorf("trip circuit breaker: %w", err)
	}
	if err := advanceControlTx(ctx, tx, input.ProjectID, input.ExpectedRevision, lastAction, now); err != nil {
		return AutopilotStatus{}, err
	}
	if err := tx.Commit(); err != nil {
		return AutopilotStatus{}, err
	}
	return s.Status(ctx, input.ProjectID)
}

func (s *OperationsService) AcknowledgeBreaker(ctx context.Context, input BreakerTransitionInput) (AutopilotStatus, error) {
	return s.transitionBreaker(ctx, input, BreakerAcknowledged)
}

func (s *OperationsService) ResolveBreaker(ctx context.Context, input BreakerTransitionInput) (AutopilotStatus, error) {
	return s.transitionBreaker(ctx, input, BreakerResolved)
}

func (s *OperationsService) transitionBreaker(ctx context.Context, input BreakerTransitionInput, target string) (AutopilotStatus, error) {
	input.BreakerID = strings.TrimSpace(input.BreakerID)
	if input.ProjectID <= 0 || input.ExpectedRevision < 0 || !identifierPattern.MatchString(input.BreakerID) {
		return AutopilotStatus{}, fmt.Errorf("invalid circuit breaker transition identity")
	}
	if _, err := s.store.ProjectProfile(ctx, input.ProjectID); err != nil {
		return AutopilotStatus{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AutopilotStatus{}, err
	}
	defer tx.Rollback()
	control, err := ensureControlTx(ctx, tx, input.ProjectID, s.store.now())
	if err != nil {
		return AutopilotStatus{}, err
	}
	lastAction := target + ":" + input.BreakerID
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM autopilot_circuit_breakers WHERE id=? AND project_id=?`, input.BreakerID, input.ProjectID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AutopilotStatus{}, ErrNotFound
		}
		return AutopilotStatus{}, err
	}
	if control.Revision == input.ExpectedRevision+1 && control.LastAction == lastAction && current == target {
		if err := tx.Commit(); err != nil {
			return AutopilotStatus{}, err
		}
		return s.Status(ctx, input.ProjectID)
	}
	if control.Revision != input.ExpectedRevision {
		return AutopilotStatus{}, ErrConflict
	}
	if target == BreakerAcknowledged && current != BreakerOpen {
		return AutopilotStatus{}, ErrConflict
	}
	if target == BreakerResolved && current != BreakerOpen && current != BreakerAcknowledged {
		return AutopilotStatus{}, ErrConflict
	}
	now := s.store.now().UTC()
	column := "acknowledged_at"
	if target == BreakerResolved {
		column = "resolved_at"
	}
	query := `UPDATE autopilot_circuit_breakers SET status=?,updated_at=?,` + column + `=? WHERE id=? AND project_id=? AND status=?`
	result, err := tx.ExecContext(ctx, query, target, stamp(now), stamp(now), input.BreakerID, input.ProjectID, current)
	if err != nil {
		return AutopilotStatus{}, fmt.Errorf("transition circuit breaker: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return AutopilotStatus{}, ErrConflict
	}
	if err := advanceControlTx(ctx, tx, input.ProjectID, input.ExpectedRevision, lastAction, now); err != nil {
		return AutopilotStatus{}, err
	}
	if err := tx.Commit(); err != nil {
		return AutopilotStatus{}, err
	}
	return s.Status(ctx, input.ProjectID)
}

func validateBreakerTrip(input BreakerTripInput) error {
	if input.ProjectID <= 0 || input.ExpectedRevision < 0 || !validBreakerCode(input.Code) || input.Reason == "" || len(input.Reason) > 2000 || len(input.Evidence) > 4000 {
		return fmt.Errorf("invalid circuit breaker payload")
	}
	if input.ScopeKind != BreakerScopeProject && input.ScopeKind != BreakerScopeLane {
		return fmt.Errorf("circuit breaker scope must be project or lane")
	}
	if input.ScopeKind == BreakerScopeProject && input.LaneKey != "" {
		return fmt.Errorf("project circuit breaker must not include a lane")
	}
	if input.ScopeKind == BreakerScopeLane && !identifierPattern.MatchString(input.LaneKey) {
		return fmt.Errorf("lane circuit breaker requires a valid lane key")
	}
	if input.ExpectedHead != "" && !fullSHA.MatchString(input.ExpectedHead) {
		return fmt.Errorf("circuit breaker expected Head must be empty or a full SHA")
	}
	return nil
}

func validBreakerCode(code string) bool {
	switch code {
	case BreakerLibraryResolutionFailure, BreakerChatGPTProjectScopeMismatch, BreakerAmbiguousWorkerResult,
		BreakerStaleCandidateHead, BreakerMergeReadbackMismatch, BreakerGitHubSynchronization,
		BreakerMissingExactHeadCI, BreakerWorkerSessionConflict, BreakerUncertainBrowserSend,
		BreakerProvisioningConflict, BreakerRepeatedBoundedFailure:
		return true
	default:
		return false
	}
}

func breakerRecovery(code string) string {
	switch code {
	case BreakerGitHubSynchronization:
		return "Restore a healthy GitHub synchronization and verify a fresh completed snapshot before resolving."
	case BreakerMissingExactHeadCI:
		return "Obtain conclusive CI for the exact Candidate Head before resolving."
	case BreakerStaleCandidateHead, BreakerMergeReadbackMismatch:
		return "Re-read the PR and Issue from GitHub, preserve the exact approved Head, and dispatch a fresh bounded correction if identities differ."
	case BreakerUncertainBrowserSend:
		return "Inspect durable browser delivery evidence. Never resend automatically; resolve only after the exact command outcome is known."
	case BreakerLibraryResolutionFailure, BreakerChatGPTProjectScopeMismatch:
		return "Restore the exact ChatGPT Project and ordered Library attachment scope, then create a fresh provisioning attempt."
	case BreakerWorkerSessionConflict, BreakerProvisioningConflict:
		return "Retire the conflicting managed session or provisioning identity and verify one current binding for the affected lane."
	case BreakerAmbiguousWorkerResult:
		return "Resolve conflicting correlated result evidence against GitHub facts and the immutable Workflow Command."
	default:
		return "Inspect the repeated bounded failure evidence, correct the underlying condition, and verify a fresh successful attempt."
	}
}

func deterministicBreakerID(projectID int64, scope, lane, code string, revision int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%d", projectID, scope, lane, code, revision)))
	return "breaker-" + hex.EncodeToString(sum[:16])
}

func ensureControlTx(ctx context.Context, tx *sql.Tx, projectID int64, now time.Time) (AutopilotControl, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO autopilot_controls(project_id,revision,last_action,updated_at) VALUES(?,0,'none',?) ON CONFLICT(project_id) DO NOTHING`, projectID, stamp(now)); err != nil {
		return AutopilotControl{}, fmt.Errorf("ensure Autopilot control: %w", err)
	}
	return scanControl(tx.QueryRowContext(ctx, `SELECT project_id,revision,last_action,updated_at FROM autopilot_controls WHERE project_id=?`, projectID))
}

func advanceControlTx(ctx context.Context, tx *sql.Tx, projectID, expectedRevision int64, action string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE autopilot_controls SET revision=revision+1,last_action=?,updated_at=? WHERE project_id=? AND revision=?`, action, stamp(now), projectID, expectedRevision)
	if err != nil {
		return fmt.Errorf("advance Autopilot control revision: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrConflict
	}
	return nil
}

func scanControl(row rowScanner) (AutopilotControl, error) {
	var value AutopilotControl
	var updated string
	if err := row.Scan(&value.ProjectID, &value.Revision, &value.LastAction, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AutopilotControl{}, ErrNotFound
		}
		return AutopilotControl{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return AutopilotControl{}, err
	}
	value.UpdatedAt = parsed
	return value, nil
}

func activeBreakerScopesTx(ctx context.Context, tx *sql.Tx, projectID int64) (bool, map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT scope_kind,lane_key FROM autopilot_circuit_breakers WHERE project_id=? AND status IN ('open','acknowledged') ORDER BY created_at,id`, projectID)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	projectBlocked := false
	lanes := make(map[string]bool)
	for rows.Next() {
		var scope, lane string
		if err := rows.Scan(&scope, &lane); err != nil {
			return false, nil, err
		}
		if scope == BreakerScopeProject {
			projectBlocked = true
		} else if lane != "" {
			lanes[lane] = true
		}
	}
	return projectBlocked, lanes, rows.Err()
}
