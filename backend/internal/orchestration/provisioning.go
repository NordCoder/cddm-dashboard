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
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
)

const maxProvisionClaimTTL = 30 * time.Minute

type ProvisioningService struct {
	store     *Store
	resources resourcepack.Package
	profiles  map[string]resourcepack.AttachmentSelection
	now       func() time.Time
}

func NewProvisioningService(store *Store, resources resourcepack.Package) (*ProvisioningService, error) {
	if store == nil || resources.Profile != ContinuousResourceProfile {
		return nil, fmt.Errorf("session provisioning requires the continuous v2 resource package")
	}
	profiles := make(map[string]resourcepack.AttachmentSelection, 3)
	for _, role := range []string{"lead", "implementor", "qa"} {
		selection, err := resources.BootstrapAttachments(role)
		if err != nil {
			return nil, err
		}
		profiles[role] = selection
	}
	return &ProvisioningService{store: store, resources: resources, profiles: profiles, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *ProvisioningService) Enqueue(ctx context.Context, input EnqueueProvisioningInput) (ProvisionRequest, error) {
	input.LeaseID = strings.TrimSpace(input.LeaseID)
	input.LeaseOwner = strings.TrimSpace(input.LeaseOwner)
	input.LeaseToken = strings.TrimSpace(input.LeaseToken)
	if input.ProjectID <= 0 || !identifierPattern.MatchString(input.LeaseID) || input.LeaseOwner == "" || input.LeaseToken == "" {
		return ProvisionRequest{}, fmt.Errorf("invalid provisioning enqueue identity")
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ProvisionRequest{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	lease, err := leaseTx(ctx, tx, input.ProjectID, input.LeaseID)
	if err != nil {
		return ProvisionRequest{}, err
	}
	if lease.Status != LeaseActive || lease.LeaseOwner != input.LeaseOwner || lease.LeaseToken != input.LeaseToken || !lease.ExpiresAt.After(now) {
		return ProvisionRequest{}, ErrConflict
	}
	intent, err := scanIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE id=? AND project_id=?`, lease.IntentID, input.ProjectID))
	if err != nil {
		return ProvisionRequest{}, err
	}
	if intent.Status != IntentClaimed || intent.LaneKey != lease.LaneKey || intent.IssueNumber <= 0 || (intent.Role != "lead" && intent.Role != "implementor" && intent.Role != "qa") {
		return ProvisionRequest{}, ErrConflict
	}
	profile, err := projectProfileTx(ctx, tx, input.ProjectID)
	if err != nil {
		return ProvisionRequest{}, err
	}
	if profile.AutonomyMode != AutonomyModeContinuous || profile.ResourceProfile != ContinuousResourceProfile {
		return ProvisionRequest{}, ErrConflict
	}
	selection := s.profiles[intent.Role]
	attachmentsJSON, err := json.Marshal(selection.Files)
	if err != nil {
		return ProvisionRequest{}, err
	}
	bindingVersion, err := currentBindingVersionTx(ctx, tx, input.ProjectID, intent.LaneKey)
	if err != nil {
		return ProvisionRequest{}, err
	}
	request := ProvisionRequest{
		ID:                     deterministicProvisionID(input.ProjectID, intent.ID),
		ProjectID:              input.ProjectID,
		IntentID:               intent.ID,
		LaneLeaseID:            lease.ID,
		LaneKey:                intent.LaneKey,
		IssueNumber:            intent.IssueNumber,
		Role:                   intent.Role,
		ExpectedHead:           provisionExpectedHead(intent),
		AttachmentProfile:      selection.Profile,
		Attachments:            append([]string(nil), selection.Files...),
		BootstrapText:          provisionBootstrap(intent),
		SessionPolicy:          provisionSessionPolicy(intent.Role),
		ChatGPTProjectURL:      profile.ChatGPTProjectURL,
		ExpectedBindingVersion: bindingVersion,
		Status:                 ProvisionPending,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if existing, readErr := provisionByIntentTx(ctx, tx, input.ProjectID, intent.ID); readErr == nil {
		if sameProvisionIdentity(existing, request) {
			if err := tx.Commit(); err != nil {
				return ProvisionRequest{}, err
			}
			return existing, nil
		}
		return ProvisionRequest{}, ErrConflict
	} else if !errors.Is(readErr, ErrNotFound) {
		return ProvisionRequest{}, readErr
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO session_provision_requests (
		id,project_id,intent_id,lane_lease_id,lane_key,issue_number,role,expected_head,attachment_profile,
		attachments_json,bootstrap_text,session_policy,chatgpt_project_url,expected_binding_version,status,
		created_at,updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		request.ID, request.ProjectID, request.IntentID, request.LaneLeaseID, request.LaneKey, request.IssueNumber,
		request.Role, request.ExpectedHead, request.AttachmentProfile, string(attachmentsJSON), request.BootstrapText,
		request.SessionPolicy, request.ChatGPTProjectURL, request.ExpectedBindingVersion, request.Status,
		stamp(now), stamp(now))
	if err != nil {
		return ProvisionRequest{}, fmt.Errorf("create session provision request: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ProvisionRequest{}, err
	}
	return s.Get(ctx, request.ID)
}

func (s *ProvisioningService) ClaimNext(ctx context.Context, input ProvisionClaimInput) (*ProvisionRequest, error) {
	input.ClaimID = strings.TrimSpace(input.ClaimID)
	input.ClaimOwner = strings.TrimSpace(input.ClaimOwner)
	if !identifierPattern.MatchString(input.ClaimID) || !identifierPattern.MatchString(input.ClaimOwner) {
		return nil, fmt.Errorf("invalid provisioning claim identity")
	}
	if input.ClaimTTL <= 0 || input.ClaimTTL > maxProvisionClaimTTL {
		return nil, fmt.Errorf("provisioning claim TTL must be positive and at most 30 minutes")
	}
	if existing, err := s.byClaim(ctx, input.ClaimID); err == nil {
		if existing.ClaimOwner != input.ClaimOwner {
			return nil, ErrConflict
		}
		return &existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		request, err := s.claimOnce(ctx, input)
		if err == nil {
			return request, nil
		}
		lastErr = err
		if !strings.Contains(strings.ToLower(err.Error()), "database is locked") {
			return nil, err
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	return nil, lastErr
}

func (s *ProvisioningService) claimOnce(ctx context.Context, input ProvisionClaimInput) (*ProvisionRequest, error) {
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	_, err = tx.ExecContext(ctx, `UPDATE session_provision_requests SET
		status='pending',claim_id='',claim_owner='',claim_token='',claim_expires_at='',updated_at=?
		WHERE status='claimed' AND claim_expires_at<>'' AND claim_expires_at<=?`, stamp(now), stamp(now))
	if err != nil {
		return nil, fmt.Errorf("expire provisioning claims: %w", err)
	}
	if existing, readErr := scanProvision(tx.QueryRowContext(ctx, provisionSelect+` WHERE status='surface_ready' AND claim_owner=? ORDER BY created_at,id LIMIT 1`, input.ClaimOwner)); readErr == nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &existing, nil
	} else if !errors.Is(readErr, ErrNotFound) {
		return nil, readErr
	}
	var requestID string
	err = tx.QueryRowContext(ctx, `SELECT r.id FROM session_provision_requests r
		JOIN project_execution_profiles p ON p.project_id=r.project_id
		WHERE r.status='pending' AND p.autonomy_mode=? AND p.autonomy_state=?
		ORDER BY r.created_at,r.id LIMIT 1`, AutonomyModeContinuous, AutonomyStateEnabled).Scan(&requestID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	token := randomProvisionToken()
	expires := now.Add(input.ClaimTTL)
	result, err := tx.ExecContext(ctx, `UPDATE session_provision_requests SET
		status='claimed',claim_id=?,claim_owner=?,claim_token=?,claim_expires_at=?,updated_at=?
		WHERE id=? AND status='pending'`, input.ClaimID, input.ClaimOwner, token, stamp(expires), stamp(now), requestID)
	if err != nil {
		return nil, fmt.Errorf("claim session provision request: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return nil, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	value, err := s.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

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
	if input.Outcome == ProvisionSurfaceReady {
		if !identifierPattern.MatchString(workerID) || tabID <= 0 {
			return ProvisionRequest{}, fmt.Errorf("surface_ready requires exact worker and tab identity")
		}
	}
	if input.Outcome == ProvisionProvisioned {
		if !identifierPattern.MatchString(workerID) || tabID <= 0 || target == nil {
			return ProvisionRequest{}, fmt.Errorf("provisioned requires exact worker, tab and conversation target")
		}
	}
	if input.Outcome == ProvisionSurfaceReady && len(input.AttachmentEvidence) != 0 {
		return ProvisionRequest{}, fmt.Errorf("surface_ready cannot assert attachment evidence")
	}
	if input.Outcome == ProvisionProvisioned && !equalProvisionStrings(current.Attachments, input.AttachmentEvidence) {
		return ProvisionRequest{}, fmt.Errorf("provisioned attachment evidence must exactly match the frozen profile")
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
	case ProvisionSurfaceReady, ProvisionProvisioned, ProvisionSafeFailed, ProvisionUncertain, ProvisionSuperseded:
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

const provisionSelect = `SELECT id,project_id,intent_id,lane_lease_id,lane_key,issue_number,role,expected_head,
	attachment_profile,attachments_json,bootstrap_text,session_policy,chatgpt_project_url,expected_binding_version,status,
	claim_id,claim_owner,claim_token,claim_expires_at,worker_id,tab_id,target_kind,target_origin,target_path,
	completion_reason,attachment_evidence_json,created_at,updated_at,completed_at FROM session_provision_requests`

func scanProvision(row rowScanner) (ProvisionRequest, error) {
	var value ProvisionRequest
	var attachmentsJSON, evidenceJSON string
	var claimExpires, targetKind, targetOrigin, targetPath, created, updated, completed string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IntentID, &value.LaneLeaseID, &value.LaneKey,
		&value.IssueNumber, &value.Role, &value.ExpectedHead, &value.AttachmentProfile, &attachmentsJSON,
		&value.BootstrapText, &value.SessionPolicy, &value.ChatGPTProjectURL, &value.ExpectedBindingVersion,
		&value.Status, &value.ClaimID, &value.ClaimOwner, &value.ClaimToken, &claimExpires, &value.WorkerID,
		&value.TabID, &targetKind, &targetOrigin, &targetPath, &value.CompletionReason, &evidenceJSON,
		&created, &updated, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProvisionRequest{}, ErrNotFound
		}
		return ProvisionRequest{}, err
	}
	if err := json.Unmarshal([]byte(attachmentsJSON), &value.Attachments); err != nil {
		return ProvisionRequest{}, err
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &value.AttachmentEvidence); err != nil {
		return ProvisionRequest{}, err
	}
	var err error
	value.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return ProvisionRequest{}, err
	}
	value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return ProvisionRequest{}, err
	}
	if claimExpires != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, claimExpires)
		if parseErr != nil {
			return ProvisionRequest{}, parseErr
		}
		value.ClaimExpiresAt = &parsed
	}
	if completed != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, completed)
		if parseErr != nil {
			return ProvisionRequest{}, parseErr
		}
		value.CompletedAt = &parsed
	}
	if targetKind != "" || targetOrigin != "" || targetPath != "" {
		target, normalizeErr := browserbinding.NormalizeTarget(browserbinding.TargetRef{Kind: targetKind, Origin: targetOrigin, Path: targetPath})
		if normalizeErr != nil {
			return ProvisionRequest{}, normalizeErr
		}
		value.Target = &target
	}
	return value, nil
}
