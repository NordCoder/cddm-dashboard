package orchestration

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
)

const provisionSelect = `SELECT id,project_id,intent_id,lane_lease_id,lane_key,issue_number,role,expected_head,
	attachment_profile,attachments_json,bootstrap_text,session_policy,chatgpt_project_url,expected_binding_version,status,
	claim_id,claim_owner,claim_token,claim_expires_at,worker_id,worker_session_id,tab_id,target_kind,target_origin,target_path,
	observed_chatgpt_url,bound_binding_id,bound_binding_version,completion_reason,attachment_evidence_json,
	created_at,updated_at,completed_at FROM session_provision_requests`

func scanProvision(row rowScanner) (ProvisionRequest, error) {
	var value ProvisionRequest
	var attachmentsJSON, evidenceJSON string
	var claimExpires, targetKind, targetOrigin, targetPath, created, updated, completed string
	if err := row.Scan(&value.ID, &value.ProjectID, &value.IntentID, &value.LaneLeaseID, &value.LaneKey,
		&value.IssueNumber, &value.Role, &value.ExpectedHead, &value.AttachmentProfile, &attachmentsJSON,
		&value.BootstrapText, &value.SessionPolicy, &value.ChatGPTProjectURL, &value.ExpectedBindingVersion,
		&value.Status, &value.ClaimID, &value.ClaimOwner, &value.ClaimToken, &claimExpires, &value.WorkerID,
		&value.WorkerSessionID, &value.TabID, &targetKind, &targetOrigin, &targetPath, &value.ObservedChatGPTURL,
		&value.BoundBindingID, &value.BoundBindingVersion, &value.CompletionReason, &evidenceJSON,
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
