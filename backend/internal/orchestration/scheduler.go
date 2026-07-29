package orchestration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

const (
	LeaseActive     = "active"
	LeaseReleased   = "released"
	LeaseCompleted  = "completed"
	LeaseSuperseded = "superseded"
	LeaseExpired    = "expired"
)

type Lease struct {
	ID         string    `json:"lease_id"`
	ProjectID  int64     `json:"project_id"`
	LaneKey    string    `json:"lane_key"`
	IntentID   string    `json:"intent_id"`
	ClaimID    string    `json:"claim_id"`
	LeaseOwner string    `json:"lease_owner"`
	LeaseToken string    `json:"lease_token"`
	Status     string    `json:"status"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
}

type ClaimRequest struct {
	ProjectID  int64
	ClaimID    string
	LeaseOwner string
	LeaseTTL   time.Duration
	Snapshot   supervisor.ProjectSnapshot
}

type ClaimDecision struct {
	Claimed bool   `json:"claimed"`
	Reason  string `json:"reason,omitempty"`
	Intent  *Intent `json:"intent,omitempty"`
	Lease   *Lease `json:"lease,omitempty"`
}

type LeaseTransition struct {
	ProjectID  int64
	LeaseID    string
	LeaseOwner string
	LeaseToken string
	Target     string
}

type Scheduler struct {
	store *Store
	now   func() time.Time
}

func NewScheduler(store *Store) *Scheduler {
	return &Scheduler{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Scheduler) ClaimNext(ctx context.Context, request ClaimRequest) (ClaimDecision, error) {
	request.ClaimID = strings.TrimSpace(request.ClaimID)
	request.LeaseOwner = strings.TrimSpace(request.LeaseOwner)
	if request.ProjectID <= 0 || request.Snapshot.Project.ID != request.ProjectID || !identifierPattern.MatchString(request.ClaimID) || !identifierPattern.MatchString(request.LeaseOwner) {
		return ClaimDecision{}, fmt.Errorf("invalid scheduler claim identity")
	}
	if request.LeaseTTL <= 0 || request.LeaseTTL > 24*time.Hour {
		return ClaimDecision{}, fmt.Errorf("lease TTL must be positive and at most 24 hours")
	}
	if existing, err := s.LeaseByClaim(ctx, request.ProjectID, request.ClaimID); err == nil {
		return decisionForExistingClaim(ctx, s.store, existing)
	} else if !errors.Is(err, ErrNotFound) {
		return ClaimDecision{}, err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		decision, err := s.claimOnce(ctx, request)
		if err == nil {
			return decision, nil
		}
		lastErr = err
		if !strings.Contains(strings.ToLower(err.Error()), "database is locked") {
			return ClaimDecision{}, err
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	return ClaimDecision{}, lastErr
}

func (s *Scheduler) claimOnce(ctx context.Context, request ClaimRequest) (ClaimDecision, error) {
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ClaimDecision{}, fmt.Errorf("begin scheduler claim: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if err := expireLeasesTx(ctx, tx, request.ProjectID, now); err != nil {
		return ClaimDecision{}, err
	}
	profile, err := projectProfileTx(ctx, tx, request.ProjectID)
	if err != nil {
		return ClaimDecision{}, err
	}
	if profile.AutonomyMode != AutonomyModeContinuous || profile.AutonomyState != AutonomyStateEnabled {
		if err := tx.Commit(); err != nil {
			return ClaimDecision{}, err
		}
		return ClaimDecision{Reason: "autonomy_not_enabled"}, nil
	}

	blockedProject, blockedIssues, err := blockedScopesTx(ctx, tx, request.ProjectID)
	if err != nil {
		return ClaimDecision{}, err
	}
	if blockedProject {
		if err := tx.Commit(); err != nil {
			return ClaimDecision{}, err
		}
		return ClaimDecision{Reason: "project_blocked"}, nil
	}
	active, err := activeStateTx(ctx, tx, request.ProjectID, profile.ControlIssueNumber)
	if err != nil {
		return ClaimDecision{}, err
	}
	pending, err := intentsTx(ctx, tx, request.ProjectID, IntentPending)
	if err != nil {
		return ClaimDecision{}, err
	}
	for _, intent := range pending {
		if intent.ActionType == ActionHold || intent.ActionType == ActionOwnerRequired {
			if err := setIntentStatusTx(ctx, tx, intent.ID, IntentPending, IntentBlocked, now); err != nil {
				return ClaimDecision{}, err
			}
			continue
		}
		if blockedIssues[intent.IssueNumber] {
			continue
		}
		if staleIntent(intent, profile, request.Snapshot) {
			if err := setIntentStatusTx(ctx, tx, intent.ID, IntentPending, IntentSuperseded, now); err != nil {
				return ClaimDecision{}, err
			}
			continue
		}
		if intent.LaneKey == "" || active.lanes[intent.LaneKey] {
			continue
		}
		if intent.IssueNumber > 0 && intent.ActionType != ActionPlanNextWave {
			if active.issues[intent.IssueNumber] {
				continue
			}
			if len(active.workIssues) >= profile.MaxActiveWorkUnits {
				continue
			}
		}
		if intent.Role == "implementor" && active.implementors >= profile.MaxParallelImplementors {
			continue
		}
		if intent.Role == "qa" && active.qa >= profile.MaxParallelQA {
			continue
		}

		lease := Lease{
			ID: deterministicLeaseID(request.ProjectID, request.ClaimID), ProjectID: request.ProjectID,
			LaneKey: intent.LaneKey, IntentID: intent.ID, ClaimID: request.ClaimID,
			LeaseOwner: request.LeaseOwner, LeaseToken: randomLeaseToken(), Status: LeaseActive,
			AcquiredAt: now, ExpiresAt: now.Add(request.LeaseTTL),
		}
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO workflow_lane_leases(id,project_id,lane_key,intent_id,claim_id,lease_owner,lease_token,status,acquired_at,expires_at,released_at) VALUES(?,?,?,?,?,?,?,'active',?,?, '')`, lease.ID, lease.ProjectID, lease.LaneKey, lease.IntentID, lease.ClaimID, lease.LeaseOwner, lease.LeaseToken, stamp(lease.AcquiredAt), stamp(lease.ExpiresAt))
		if err != nil {
			return ClaimDecision{}, fmt.Errorf("create lane lease: %w", err)
		}
		inserted, _ := result.RowsAffected()
		if inserted != 1 {
			continue
		}
		updated, err := tx.ExecContext(ctx, `UPDATE workflow_intents SET status='claimed',updated_at=? WHERE id=? AND project_id=? AND status='pending'`, stamp(now), intent.ID, request.ProjectID)
		if err != nil {
			return ClaimDecision{}, fmt.Errorf("claim Workflow Intent: %w", err)
		}
		count, _ := updated.RowsAffected()
		if count != 1 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_lane_leases WHERE id=?`, lease.ID); err != nil {
				return ClaimDecision{}, err
			}
			continue
		}
		intent.Status = IntentClaimed
		intent.UpdatedAt = now
		if err := tx.Commit(); err != nil {
			return ClaimDecision{}, fmt.Errorf("commit scheduler claim: %w", err)
		}
		return ClaimDecision{Claimed: true, Intent: &intent, Lease: &lease}, nil
	}
	if err := tx.Commit(); err != nil {
		return ClaimDecision{}, err
	}
	return ClaimDecision{Reason: "no_runnable_intent"}, nil
}

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
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_lane_leases SET status=?,released_at=? WHERE id=? AND project_id=? AND status='active'`, request.Target, stamp(now), request.LeaseID, request.ProjectID); err != nil {
		return Lease{}, fmt.Errorf("transition lane lease: %w", err)
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

func staleIntent(intent Intent, profile ProjectProfile, snapshot supervisor.ProjectSnapshot) bool {
	if !strings.EqualFold(intent.Repository, snapshot.Project.Owner+"/"+snapshot.Project.Repository) {
		return true
	}
	if intent.ActionType == ActionPlanNextWave && intent.IssueNumber != profile.ControlIssueNumber {
		return true
	}
	if intent.IssueNumber == 0 {
		return intent.ActionType != ActionHold && intent.ActionType != ActionOwnerRequired
	}
	issue, ok := snapshotIssue(snapshot, intent.IssueNumber)
	if !ok {
		return true
	}
	switch intent.ActionType {
	case ActionDispatch:
		return intent.Role == "qa" && !issueHasHead(issue, intent.ExpectedHead)
	case ActionCorrect:
		return intent.ExpectedPreviousHead != "" && !issueHasHead(issue, intent.ExpectedPreviousHead)
	case ActionMerge:
		return !issueHasPRHead(issue, intent.PRNumber, intent.ExpectedHead)
	}
	return false
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

func deterministicLeaseID(projectID int64, claimID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", projectID, claimID)))
	return "lease-" + hex.EncodeToString(sum[:16])
}

func randomLeaseToken() string {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		panic(fmt.Sprintf("generate lease token: %v", err))
	}
	return hex.EncodeToString(value)
}
