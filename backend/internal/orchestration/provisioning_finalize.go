package orchestration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
)

type ProvisioningFinalizer struct {
	store    *Store
	bindings *browserbinding.Service
	now      func() time.Time
}

func NewProvisioningFinalizer(store *Store, bindings *browserbinding.Service) (*ProvisioningFinalizer, error) {
	if store == nil || bindings == nil {
		return nil, fmt.Errorf("provisioning finalizer requires store and browser bindings")
	}
	return &ProvisioningFinalizer{store: store, bindings: bindings, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *ProvisioningFinalizer) Finalize(ctx context.Context, input FinalizeProvisioningInput) (ProvisionRequest, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.ClaimOwner = strings.TrimSpace(input.ClaimOwner)
	input.ClaimToken = strings.TrimSpace(input.ClaimToken)
	input.WorkerID = strings.TrimSpace(input.WorkerID)
	input.ObservedChatGPTURL = strings.TrimSpace(input.ObservedChatGPTURL)
	if !identifierPattern.MatchString(input.RequestID) || !identifierPattern.MatchString(input.WorkerID) || input.ClaimOwner == "" || input.ClaimToken == "" || input.TabID <= 0 {
		return ProvisionRequest{}, fmt.Errorf("invalid provisioning finalize identity")
	}
	target, err := browserbinding.NormalizeTarget(input.Target)
	if err != nil {
		return ProvisionRequest{}, err
	}
	workerSessionID, err := s.bindings.RequireFreshTargetSession(input.WorkerID, target)
	if err != nil {
		return ProvisionRequest{}, ErrConflict
	}

	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ProvisionRequest{}, err
	}
	defer tx.Rollback()
	request, err := scanProvision(tx.QueryRowContext(ctx, provisionSelect+` WHERE id=?`, input.RequestID))
	if err != nil {
		return ProvisionRequest{}, err
	}
	if request.Status != ProvisionSurfaceReady || request.ClaimOwner != input.ClaimOwner || request.ClaimToken != input.ClaimToken || request.WorkerID != input.WorkerID || request.TabID != input.TabID {
		return ProvisionRequest{}, ErrConflict
	}
	if !equalProvisionStrings(request.Attachments, input.AttachmentEvidence) {
		return ProvisionRequest{}, fmt.Errorf("attachment evidence must exactly match the frozen profile")
	}
	observedURL, err := validateObservedConversation(input.ObservedChatGPTURL, request.ChatGPTProjectURL, target)
	if err != nil {
		return ProvisionRequest{}, err
	}
	lease, err := leaseTx(ctx, tx, request.ProjectID, request.LaneLeaseID)
	if err != nil {
		return ProvisionRequest{}, err
	}
	intent, err := scanIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE id=? AND project_id=?`, request.IntentID, request.ProjectID))
	if err != nil {
		return ProvisionRequest{}, err
	}
	if lease.Status != LeaseActive || lease.IntentID != intent.ID || lease.LaneKey != request.LaneKey || intent.Status != IntentClaimed || intent.LaneKey != request.LaneKey || intent.IssueNumber != request.IssueNumber || intent.Role != request.Role || provisionExpectedHead(intent) != request.ExpectedHead {
		return ProvisionRequest{}, ErrConflict
	}
	profile, err := projectProfileTx(ctx, tx, request.ProjectID)
	if err != nil {
		return ProvisionRequest{}, err
	}
	if profile.AutonomyMode != AutonomyModeContinuous || profile.AutonomyState != AutonomyStateEnabled || profile.ChatGPTProjectURL != request.ChatGPTProjectURL {
		return ProvisionRequest{}, ErrConflict
	}
	if err := requireCurrentGitHubFacts(ctx, tx, request.ProjectID, intent); err != nil {
		return ProvisionRequest{}, err
	}
	now := s.now().UTC()
	bindingID, bindingVersion, err := finalizeBindingTx(ctx, tx, request, input.WorkerID, target, now)
	if err != nil {
		return ProvisionRequest{}, err
	}
	if err := retireSupersededProvisioningTx(ctx, tx, request, bindingID, now); err != nil {
		return ProvisionRequest{}, err
	}
	evidenceJSON, err := json.Marshal(input.AttachmentEvidence)
	if err != nil {
		return ProvisionRequest{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE session_provision_requests SET
		status='provisioned',worker_session_id=?,target_kind=?,target_origin=?,target_path=?,observed_chatgpt_url=?,
		bound_binding_id=?,bound_binding_version=?,attachment_evidence_json=?,completion_reason='exact_session_bound',
		updated_at=?,completed_at=?
		WHERE id=? AND status='surface_ready' AND claim_owner=? AND claim_token=? AND worker_id=? AND tab_id=?`,
		workerSessionID, target.Kind, target.Origin, target.Path, observedURL, bindingID, bindingVersion, string(evidenceJSON),
		stamp(now), stamp(now), request.ID, input.ClaimOwner, input.ClaimToken, input.WorkerID, input.TabID)
	if err != nil {
		return ProvisionRequest{}, fmt.Errorf("finalize session provision request: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ProvisionRequest{}, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return ProvisionRequest{}, err
	}
	return s.storeProvision(ctx, request.ID)
}

func (s *ProvisioningFinalizer) storeProvision(ctx context.Context, requestID string) (ProvisionRequest, error) {
	return scanProvision(s.store.db.QueryRowContext(ctx, provisionSelect+` WHERE id=?`, requestID))
}

func validateObservedConversation(value, projectURL string, target browserbinding.TargetRef) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "chatgpt.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("observed ChatGPT URL is invalid")
	}
	marker := "/c/"
	index := strings.LastIndex(parsed.Path, marker)
	if index < 0 {
		return "", fmt.Errorf("observed ChatGPT URL is not a conversation")
	}
	prefix, key := parsed.Path[:index], parsed.Path[index+len(marker):]
	if key == "" || strings.Contains(key, "/") || target.Path != marker+key {
		return "", fmt.Errorf("observed ChatGPT URL does not match the exact target")
	}
	expectedPrefix := ""
	if projectURL != "" {
		project, parseErr := url.Parse(projectURL)
		if parseErr != nil {
			return "", fmt.Errorf("stored ChatGPT Project URL is invalid")
		}
		expectedPrefix = strings.TrimSuffix(strings.TrimRight(project.Path, "/"), "/project")
	}
	if prefix != expectedPrefix {
		return "", fmt.Errorf("observed ChatGPT conversation is outside the configured Project scope")
	}
	return "https://chatgpt.com" + parsed.Path, nil
}

func requireCurrentGitHubFacts(ctx context.Context, tx *sql.Tx, projectID int64, intent Intent) error {
	var syncStatus string
	if err := tx.QueryRowContext(ctx, `SELECT sync_status FROM projects WHERE id=?`, projectID).Scan(&syncStatus); err != nil {
		return err
	}
	if syncStatus != "healthy" {
		return ErrConflict
	}
	var issueID int64
	if err := tx.QueryRowContext(ctx, `SELECT github_id FROM github_issues WHERE project_id=? AND issue_number=?`, projectID, intent.IssueNumber).Scan(&issueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		return err
	}
	expectedHead := provisionExpectedHead(intent)
	if expectedHead == "" {
		return nil
	}
	query := `SELECT COUNT(*) FROM github_issue_pull_requests link
		JOIN github_pull_requests pr ON pr.project_id=link.project_id AND pr.github_id=link.pull_request_github_id
		WHERE link.project_id=? AND link.issue_github_id=? AND pr.state='open' AND pr.head_sha=?`
	args := []any{projectID, issueID, expectedHead}
	if intent.PRNumber > 0 {
		query += ` AND pr.pr_number=?`
		args = append(args, intent.PRNumber)
	}
	var count int
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func finalizeBindingTx(ctx context.Context, tx *sql.Tx, request ProvisionRequest, workerID string, target browserbinding.TargetRef, now time.Time) (string, int64, error) {
	var bindingID string
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT binding_id,binding_version FROM browser_lane_bindings WHERE project_id=? AND lane_key=?`, request.ProjectID, request.LaneKey).Scan(&bindingID, &version)
	if errors.Is(err, sql.ErrNoRows) {
		if request.ExpectedBindingVersion != 0 {
			return "", 0, ErrConflict
		}
		bindingID = randomProvisionToken()[:32]
		version = 1
		if err := requireProvisionTargetFree(ctx, tx, request.ProjectID, request.LaneKey, workerID, target); err != nil {
			return "", 0, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO browser_lane_bindings(
			binding_id,project_id,lane_key,worker_id,target_kind,target_origin,target_path,target_label,
			enabled,binding_version,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,1,1,?,?)`, bindingID, request.ProjectID, request.LaneKey, workerID,
			target.Kind, target.Origin, target.Path, "", stamp(now), stamp(now))
		if err != nil {
			return "", 0, fmt.Errorf("create provisioned role binding: %w", err)
		}
		return bindingID, version, nil
	}
	if err != nil {
		return "", 0, err
	}
	if version != request.ExpectedBindingVersion {
		return "", 0, ErrConflict
	}
	if err := requireProvisionTargetFree(ctx, tx, request.ProjectID, request.LaneKey, workerID, target); err != nil {
		return "", 0, err
	}
	version++
	result, err := tx.ExecContext(ctx, `UPDATE browser_lane_bindings SET
		worker_id=?,target_kind=?,target_origin=?,target_path=?,target_label='',enabled=1,binding_version=?,updated_at=?
		WHERE binding_id=? AND binding_version=?`, workerID, target.Kind, target.Origin, target.Path, version, stamp(now), bindingID, request.ExpectedBindingVersion)
	if err != nil {
		return "", 0, fmt.Errorf("update provisioned role binding: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return "", 0, ErrConflict
	}
	return bindingID, version, nil
}

func retireSupersededProvisioningTx(ctx context.Context, tx *sql.Tx, current ProvisionRequest, currentBindingID string, now time.Time) error {
	query := `SELECT id,bound_binding_id FROM session_provision_requests
		WHERE project_id=? AND id<>? AND status='provisioned' AND role=?`
	args := []any{current.ProjectID, current.ID, current.Role}
	if current.Role != "lead" {
		query += ` AND issue_number=?`
		args = append(args, current.IssueNumber)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list superseded provisioned sessions: %w", err)
	}
	type priorSession struct {
		requestID string
		bindingID string
	}
	prior := make([]priorSession, 0)
	for rows.Next() {
		var value priorSession
		if err := rows.Scan(&value.requestID, &value.bindingID); err != nil {
			rows.Close()
			return err
		}
		prior = append(prior, value)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, value := range prior {
		if value.bindingID != "" && value.bindingID != currentBindingID {
			if _, err := tx.ExecContext(ctx, `UPDATE browser_lane_bindings SET
				enabled=0,binding_version=binding_version+1,updated_at=?
				WHERE binding_id=? AND enabled=1`, stamp(now), value.bindingID); err != nil {
				return fmt.Errorf("disable superseded role binding: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE session_provision_requests SET
			status='superseded',completion_reason='replaced_by_current_session',updated_at=?
			WHERE id=? AND status='provisioned'`, stamp(now), value.requestID); err != nil {
			return fmt.Errorf("supersede prior role session: %w", err)
		}
	}
	return nil
}

func requireProvisionTargetFree(ctx context.Context, tx *sql.Tx, projectID int64, laneKey, workerID string, target browserbinding.TargetRef) error {
	var bindingID string
	err := tx.QueryRowContext(ctx, `SELECT binding_id FROM browser_lane_bindings
		WHERE enabled=1 AND worker_id=? AND target_kind=? AND target_origin=? AND target_path=?
		AND NOT(project_id=? AND lane_key=?)`, workerID, target.Kind, target.Origin, target.Path, projectID, laneKey).Scan(&bindingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrConflict
}
