package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Scheduler) Transition(ctx context.Context, request LeaseTransition) (Lease, error) {
	request.LeaseID = strings.TrimSpace(request.LeaseID)
	request.LeaseOwner = strings.TrimSpace(request.LeaseOwner)
	request.LeaseToken = strings.TrimSpace(request.LeaseToken)
	if request.ProjectID <= 0 || !identifierPattern.MatchString(request.LeaseID) || request.LeaseOwner == "" || request.LeaseToken == "" {
		return Lease{}, fmt.Errorf("invalid lease transition identity")
	}
	if request.Target != LeaseReleased && request.Target != LeaseCompleted && request.Target != LeaseSuperseded {
		return Lease{}, fmt.Errorf("invalid lease transition target %q", request.Target)
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Lease{}, err
	}
	defer tx.Rollback()
	lease, err := leaseTx(ctx, tx, request.ProjectID, request.LeaseID)
	if err != nil {
		return Lease{}, err
	}
	if lease.LeaseOwner != request.LeaseOwner || lease.LeaseToken != request.LeaseToken {
		return Lease{}, ErrConflict
	}
	if lease.Status == request.Target {
		if err := tx.Commit(); err != nil {
			return Lease{}, err
		}
		return lease, nil
	}
	if lease.Status != LeaseActive {
		return Lease{}, ErrConflict
	}
	now := s.now().UTC()
	intentStatus := IntentPending
	switch request.Target {
	case LeaseCompleted:
		intentStatus = IntentCompleted
	case LeaseSuperseded:
		intentStatus = IntentSuperseded
	}
	if err := setIntentStatusTx(ctx, tx, lease.IntentID, IntentClaimed, intentStatus, now); err != nil {
		return Lease{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE workflow_lane_leases SET status=?,released_at=? WHERE id=? AND project_id=? AND status='active'`, request.Target, stamp(now), request.LeaseID, request.ProjectID)
	if err != nil {
		return Lease{}, fmt.Errorf("transition lane lease: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return Lease{}, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return Lease{}, err
	}
	return s.Lease(ctx, request.ProjectID, request.LeaseID)
}

func (s *Scheduler) Lease(ctx context.Context, projectID int64, leaseID string) (Lease, error) {
	return scanLease(s.store.db.QueryRowContext(ctx, leaseSelect+` WHERE project_id=? AND id=?`, projectID, strings.TrimSpace(leaseID)))
}

func (s *Scheduler) LeaseByClaim(ctx context.Context, projectID int64, claimID string) (Lease, error) {
	return scanLease(s.store.db.QueryRowContext(ctx, leaseSelect+` WHERE project_id=? AND claim_id=?`, projectID, strings.TrimSpace(claimID)))
}

func (s *Scheduler) ListLeases(ctx context.Context, projectID int64) ([]Lease, error) {
	rows, err := s.store.db.QueryContext(ctx, leaseSelect+` WHERE project_id=? ORDER BY acquired_at,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Lease, 0)
	for rows.Next() {
		value, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

type activeSchedulerState struct {
	lanes        map[string]bool
	issues       map[int]bool
	workIssues   map[int]bool
	implementors int
	qa           int
}

func activeStateTx(ctx context.Context, tx *sql.Tx, projectID int64, controlIssue int) (activeSchedulerState, error) {
	state := activeSchedulerState{lanes: make(map[string]bool), issues: make(map[int]bool), workIssues: make(map[int]bool)}
	rows, err := tx.QueryContext(ctx, `SELECT l.lane_key,i.issue_number,i.role,i.action_type FROM workflow_lane_leases l JOIN workflow_intents i ON i.id=l.intent_id WHERE l.project_id=? AND l.status='active' AND i.status='claimed'`, projectID)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var lane, role, action string
		var issue int
		if err := rows.Scan(&lane, &issue, &role, &action); err != nil {
			return state, err
		}
		state.lanes[lane] = true
		if issue > 0 && action != ActionPlanNextWave {
			state.issues[issue] = true
			if issue != controlIssue {
				state.workIssues[issue] = true
			}
		}
		if role == "implementor" {
			state.implementors++
		}
		if role == "qa" {
			state.qa++
		}
	}
	return state, rows.Err()
}

func blockedScopesTx(ctx context.Context, tx *sql.Tx, projectID int64) (bool, map[int]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT issue_number FROM workflow_intents WHERE project_id=? AND status='blocked' AND action_type IN ('hold','owner_required')`, projectID)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	issues := make(map[int]bool)
	projectBlocked := false
	for rows.Next() {
		var issue int
		if err := rows.Scan(&issue); err != nil {
			return false, nil, err
		}
		if issue == 0 {
			projectBlocked = true
		} else {
			issues[issue] = true
		}
	}
	return projectBlocked, issues, rows.Err()
}

func expireLeasesTx(ctx context.Context, tx *sql.Tx, projectID int64, now time.Time) error {
	rows, err := tx.QueryContext(ctx, leaseSelect+` WHERE project_id=? AND status='active' ORDER BY acquired_at,id`, projectID)
	if err != nil {
		return err
	}
	active := make([]Lease, 0)
	for rows.Next() {
		lease, err := scanLease(rows)
		if err != nil {
			rows.Close()
			return err
		}
		active = append(active, lease)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, lease := range active {
		if lease.ExpiresAt.After(now) {
			continue
		}
		if err := setIntentStatusTx(ctx, tx, lease.IntentID, IntentClaimed, IntentPending, now); err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_lane_leases SET status='expired',released_at=? WHERE id=? AND status='active'`, stamp(now), lease.ID); err != nil {
			return err
		}
	}
	return nil
}

func projectProfileTx(ctx context.Context, tx *sql.Tx, projectID int64) (ProjectProfile, error) {
	var value ProjectProfile
	var autoMerge int
	var updated string
	err := tx.QueryRowContext(ctx, `SELECT project_id,resource_profile,methodology,result_protocol,delivery_mode,qa_session_mode,auto_merge,autonomy_mode,autonomy_state,control_issue_number,max_active_work_units,max_parallel_implementors,max_parallel_qa,updated_at FROM project_execution_profiles WHERE project_id=?`, projectID).Scan(&value.ProjectID, &value.ResourceProfile, &value.Methodology, &value.ResultProtocol, &value.DeliveryMode, &value.QASessionMode, &autoMerge, &value.AutonomyMode, &value.AutonomyState, &value.ControlIssueNumber, &value.MaxActiveWorkUnits, &value.MaxParallelImplementors, &value.MaxParallelQA, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectProfile{}, ErrNotFound
	}
	if err != nil {
		return ProjectProfile{}, err
	}
	value.AutoMerge = autoMerge != 0
	value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return value, err
}

func intentsTx(ctx context.Context, tx *sql.Tx, projectID int64, status string) ([]Intent, error) {
	rows, err := tx.QueryContext(ctx, intentSelect+` WHERE project_id=? AND status=? ORDER BY priority,created_at,action_id,id`, projectID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Intent, 0)
	for rows.Next() {
		value, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func setIntentStatusTx(ctx context.Context, tx *sql.Tx, intentID, from, to string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE workflow_intents SET status=?,updated_at=? WHERE id=? AND status=?`, to, stamp(now), intentID, from)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrConflict
	}
	return nil
}

const leaseSelect = `SELECT id,project_id,lane_key,intent_id,claim_id,lease_owner,lease_token,status,acquired_at,expires_at,released_at FROM workflow_lane_leases`

func leaseTx(ctx context.Context, tx *sql.Tx, projectID int64, leaseID string) (Lease, error) {
	return scanLease(tx.QueryRowContext(ctx, leaseSelect+` WHERE project_id=? AND id=?`, projectID, leaseID))
}

func scanLease(row rowScanner) (Lease, error) {
	var value Lease
	var acquired, expires, released string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.LaneKey, &value.IntentID, &value.ClaimID, &value.LeaseOwner, &value.LeaseToken, &value.Status, &acquired, &expires, &released); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Lease{}, ErrNotFound
		}
		return Lease{}, err
	}
	var err error
	value.AcquiredAt, err = time.Parse(time.RFC3339Nano, acquired)
	if err != nil {
		return Lease{}, err
	}
	value.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return Lease{}, err
	}
	if released != "" {
		parsed, err := time.Parse(time.RFC3339Nano, released)
		if err != nil {
			return Lease{}, err
		}
		value.ReleasedAt = &parsed
	}
	return value, nil
}

func decisionForExistingClaim(ctx context.Context, store *Store, lease Lease) (ClaimDecision, error) {
	intent, err := store.Intent(ctx, lease.IntentID)
	if err != nil {
		return ClaimDecision{}, err
	}
	return ClaimDecision{Claimed: lease.Status == LeaseActive, Reason: "claim_" + lease.Status, Intent: &intent, Lease: &lease}, nil
}
