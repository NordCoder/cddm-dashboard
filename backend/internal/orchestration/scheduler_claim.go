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
			ID:         deterministicLeaseID(request.ProjectID, request.ClaimID),
			ProjectID:  request.ProjectID,
			LaneKey:    intent.LaneKey,
			IntentID:   intent.ID,
			ClaimID:    request.ClaimID,
			LeaseOwner: request.LeaseOwner,
			LeaseToken: randomLeaseToken(),
			Status:     LeaseActive,
			AcquiredAt: now,
			ExpiresAt:  now.Add(request.LeaseTTL),
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
