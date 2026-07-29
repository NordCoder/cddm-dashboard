package orchestration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
