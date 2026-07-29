package orchestration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
)

func (s *ProvisioningService) Complete(ctx context.Context, input ProvisionCompletionInput) (ProvisionRequest, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.ClaimOwner = strings.TrimSpace(input.ClaimOwner)
	input.ClaimToken = strings.TrimSpace(input.ClaimToken)
	input.Reason = strings.TrimSpace(input.Reason)
	input.WorkerID = strings.TrimSpace(input.WorkerID)
	if !identifierPattern.MatchString(input.RequestID) || input.ClaimOwner == "" || input.ClaimToken == "" || len(input.Reason) > 200 {
		return ProvisionRequest{}, fmt.Errorf("invalid provisioning completion identity")
	}
	if !validProvisionOutcome(input.Outcome) {
		return ProvisionRequest{}, fmt.Errorf("invalid provisioning outcome %q", input.Outcome)
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ProvisionRequest{}, err
	}
	defer tx.Rollback()
	current, err := scanProvision(tx.QueryRowContext(ctx, provisionSelect+` WHERE id=?`, input.RequestID))
	if err != nil {
		return ProvisionRequest{}, err
	}
	if current.ClaimOwner != input.ClaimOwner || current.ClaimToken != input.ClaimToken || (current.Status != ProvisionClaimed && current.Status != ProvisionSurfaceReady) {
		return ProvisionRequest{}, ErrConflict
	}
	now := s.now().UTC()
	if current.Status == ProvisionClaimed && (current.ClaimExpiresAt == nil || !current.ClaimExpiresAt.After(now)) {
		return ProvisionRequest{}, ErrConflict
	}
	workerID, tabID, target := current.WorkerID, current.TabID, current.Target
	if input.WorkerID != "" {
		workerID = input.WorkerID
	}
	if input.TabID != 0 {
		tabID = input.TabID
	}
	if input.Target != nil {
		normalized, normalizeErr := browserbinding.NormalizeTarget(*input.Target)
		if normalizeErr != nil {
			return ProvisionRequest{}, normalizeErr
		}
		target = &normalized
	}
	if input.Outcome == ProvisionSurfaceReady && (!identifierPattern.MatchString(workerID) || tabID <= 0) {
		return ProvisionRequest{}, fmt.Errorf("surface_ready requires exact worker and tab identity")
	}
	if input.Outcome == ProvisionSurfaceReady && len(input.AttachmentEvidence) != 0 {
		return ProvisionRequest{}, fmt.Errorf("surface_ready cannot assert attachment evidence")
	}
	evidenceJSON, err := json.Marshal(input.AttachmentEvidence)
	if err != nil {
		return ProvisionRequest{}, err
	}
	targetKind, targetOrigin, targetPath := "", "", ""
	if target != nil {
		targetKind, targetOrigin, targetPath = target.Kind, target.Origin, target.Path
	}
	completedAt := ""
	if input.Outcome != ProvisionSurfaceReady {
		completedAt = stamp(now)
	}
	result, err := tx.ExecContext(ctx, `UPDATE session_provision_requests SET
		status=?,worker_id=?,tab_id=?,target_kind=?,target_origin=?,target_path=?,completion_reason=?,
		attachment_evidence_json=?,updated_at=?,completed_at=?
		WHERE id=? AND claim_owner=? AND claim_token=? AND status=?`,
		input.Outcome, workerID, tabID, targetKind, targetOrigin, targetPath, input.Reason,
		string(evidenceJSON), stamp(now), completedAt, input.RequestID, input.ClaimOwner, input.ClaimToken, current.Status)
	if err != nil {
		return ProvisionRequest{}, fmt.Errorf("complete session provision request: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ProvisionRequest{}, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return ProvisionRequest{}, err
	}
	return s.Get(ctx, input.RequestID)
}

func (s *ProvisioningService) Get(ctx context.Context, requestID string) (ProvisionRequest, error) {
	return scanProvision(s.store.db.QueryRowContext(ctx, provisionSelect+` WHERE id=?`, strings.TrimSpace(requestID)))
}

func (s *ProvisioningService) ListProject(ctx context.Context, projectID int64) ([]ProvisionRequest, error) {
	rows, err := s.store.db.QueryContext(ctx, provisionSelect+` WHERE project_id=? ORDER BY created_at,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]ProvisionRequest, 0)
	for rows.Next() {
		value, scanErr := scanProvision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *ProvisioningService) byClaim(ctx context.Context, claimID string) (ProvisionRequest, error) {
	return scanProvision(s.store.db.QueryRowContext(ctx, provisionSelect+` WHERE claim_id=?`, claimID))
}

func currentBindingVersionTx(ctx context.Context, tx *sql.Tx, projectID int64, lane string) (int64, error) {
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT binding_version FROM browser_lane_bindings WHERE project_id=? AND lane_key=?`, projectID, lane).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return version, err
}

func provisionByIntentTx(ctx context.Context, tx *sql.Tx, projectID int64, intentID string) (ProvisionRequest, error) {
	return scanProvision(tx.QueryRowContext(ctx, provisionSelect+` WHERE project_id=? AND intent_id=?`, projectID, intentID))
}

func provisionExpectedHead(intent Intent) string {
	if intent.Role == "qa" || intent.ActionType == ActionMerge {
		return intent.ExpectedHead
	}
	if intent.ActionType == ActionCorrect {
		return intent.ExpectedPreviousHead
	}
	return intent.ExpectedHead
}

func provisionSessionPolicy(role string) string {
	if role == "lead" {
		return SessionPolicyPersistentLead
	}
	return SessionPolicyFreshPerIntent
}

func provisionBootstrap(intent Intent) string {
	return fmt.Sprintf("Initialize as the CDDM %s worker for %s Issue #%d using the attached versioned resources. This is bootstrap only: do not perform repository work yet. Wait for a Workflow Command from CDDM Dashboard.", intent.Role, intent.Repository, intent.IssueNumber)
}

func deterministicProvisionID(projectID int64, intentID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", projectID, intentID)))
	return "provision-" + hex.EncodeToString(sum[:16])
}

func randomProvisionToken() string {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}

func sameProvisionIdentity(left, right ProvisionRequest) bool {
	return left.ID == right.ID && left.ProjectID == right.ProjectID && left.IntentID == right.IntentID &&
		left.LaneLeaseID == right.LaneLeaseID && left.LaneKey == right.LaneKey && left.IssueNumber == right.IssueNumber &&
		left.Role == right.Role && left.ExpectedHead == right.ExpectedHead && left.AttachmentProfile == right.AttachmentProfile &&
		equalProvisionStrings(left.Attachments, right.Attachments) && left.BootstrapText == right.BootstrapText &&
		left.SessionPolicy == right.SessionPolicy && left.ChatGPTProjectURL == right.ChatGPTProjectURL &&
		left.ExpectedBindingVersion == right.ExpectedBindingVersion
}

func validProvisionOutcome(value string) bool {
	switch value {
	case ProvisionSurfaceReady, ProvisionSafeFailed, ProvisionUncertain, ProvisionSuperseded:
		return true
	default:
		return false
	}
}

func equalProvisionStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
