package orchestration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
)

// finalizeProvisioningWithSession commits the durable provisioning and binding
// graph while browserbinding.Service keeps the exact live session snapshot
// locked. It returns the request identity only after the SQL transaction commits.
func (s *ProvisioningFinalizer) finalizeProvisioningWithSession(
	ctx context.Context,
	input FinalizeProvisioningInput,
	target browserbinding.TargetRef,
	workerSessionID string,
) (string, error) {
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	request, err := scanProvision(tx.QueryRowContext(ctx, provisionSelect+` WHERE id=?`, input.RequestID))
	if err != nil {
		return "", err
	}
	if request.Status != ProvisionSurfaceReady || request.ClaimOwner != input.ClaimOwner || request.ClaimToken != input.ClaimToken || request.WorkerID != input.WorkerID || request.TabID != input.TabID {
		return "", ErrConflict
	}
	if !equalProvisionStrings(request.Attachments, input.AttachmentEvidence) {
		return "", fmt.Errorf("attachment evidence must exactly match the frozen profile")
	}
	observedURL, err := validateObservedConversation(input.ObservedChatGPTURL, request.ChatGPTProjectURL, target)
	if err != nil {
		return "", err
	}
	lease, err := leaseTx(ctx, tx, request.ProjectID, request.LaneLeaseID)
	if err != nil {
		return "", err
	}
	intent, err := scanIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE id=? AND project_id=?`, request.IntentID, request.ProjectID))
	if err != nil {
		return "", err
	}
	if lease.Status != LeaseActive || lease.IntentID != intent.ID || lease.LaneKey != request.LaneKey || intent.Status != IntentClaimed || intent.LaneKey != request.LaneKey || intent.IssueNumber != request.IssueNumber || intent.Role != request.Role || provisionExpectedHead(intent) != request.ExpectedHead {
		return "", ErrConflict
	}
	profile, err := projectProfileTx(ctx, tx, request.ProjectID)
	if err != nil {
		return "", err
	}
	if profile.AutonomyMode != AutonomyModeContinuous || profile.AutonomyState != AutonomyStateEnabled || profile.ChatGPTProjectURL != request.ChatGPTProjectURL {
		return "", ErrConflict
	}
	if err := requireCurrentGitHubFacts(ctx, tx, request.ProjectID, intent); err != nil {
		return "", err
	}
	now := s.now().UTC()
	bindingID, bindingVersion, err := finalizeBindingTx(ctx, tx, request, input.WorkerID, target, now)
	if err != nil {
		return "", err
	}
	if err := retireSupersededProvisioningTx(ctx, tx, request, bindingID, now); err != nil {
		return "", err
	}
	evidenceJSON, err := json.Marshal(input.AttachmentEvidence)
	if err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE session_provision_requests SET
		status='provisioned',worker_session_id=?,target_kind=?,target_origin=?,target_path=?,observed_chatgpt_url=?,
		bound_binding_id=?,bound_binding_version=?,attachment_evidence_json=?,completion_reason='exact_session_bound',
		updated_at=?,completed_at=?
		WHERE id=? AND status='surface_ready' AND claim_owner=? AND claim_token=? AND worker_id=? AND tab_id=?`,
		workerSessionID, target.Kind, target.Origin, target.Path, observedURL, bindingID, bindingVersion, string(evidenceJSON),
		stamp(now), stamp(now), request.ID, input.ClaimOwner, input.ClaimToken, input.WorkerID, input.TabID)
	if err != nil {
		return "", fmt.Errorf("finalize session provision request: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return "", ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return request.ID, nil
}
