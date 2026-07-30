package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *OperationsService) Status(ctx context.Context, projectID int64) (AutopilotStatus, error) {
	if projectID <= 0 {
		return AutopilotStatus{}, fmt.Errorf("project id must be positive")
	}
	profile, err := s.store.ProjectProfile(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	control, err := s.ensureControl(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	var owner, repository, syncStatus, syncError string
	if err := s.store.db.QueryRowContext(ctx, `SELECT owner,repository,sync_status,sync_error FROM projects WHERE id=?`, projectID).Scan(&owner, &repository, &syncStatus, &syncError); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AutopilotStatus{}, ErrNotFound
		}
		return AutopilotStatus{}, err
	}

	activeWave, err := s.activeWave(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	intents, err := s.store.ListIntents(ctx, projectID, "")
	if err != nil {
		return AutopilotStatus{}, err
	}
	leases, err := s.scheduler.ListLeases(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	breakers, err := s.listBreakers(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	provisioning, err := s.listProvisioning(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	commands, err := s.listCommands(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	results, err := s.listResults(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	warnings, err := s.listWarnings(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}
	mergeCycles, err := s.listMergeCycles(ctx, projectID)
	if err != nil {
		return AutopilotStatus{}, err
	}

	activeLanes := make(map[string]bool)
	allLeases := make([]LeaseProjection, 0, len(leases))
	activeLeases := make([]LeaseProjection, 0)
	intentByID := make(map[string]Intent, len(intents))
	for _, intent := range intents {
		intentByID[intent.ID] = intent
	}
	leadBusy := false
	for _, lease := range leases {
		projected := projectLease(lease)
		allLeases = append(allLeases, projected)
		if lease.Status != LeaseActive {
			continue
		}
		activeLeases = append(activeLeases, projected)
		activeLanes[lease.LaneKey] = true
		if intentByID[lease.IntentID].Role == "lead" {
			leadBusy = true
		}
	}
	projectBreaker := false
	breakerLanes := make(map[string]bool)
	for _, breaker := range breakers {
		if breaker.Status == BreakerResolved {
			continue
		}
		if breaker.ScopeKind == BreakerScopeProject {
			projectBreaker = true
		} else {
			breakerLanes[breaker.LaneKey] = true
		}
	}

	queue := make([]AutopilotQueueItem, 0)
	projectHoldReason := ""
	counts := AutopilotCounts{ActiveLeases: len(activeLeases)}
	for _, intent := range intents {
		switch intent.Status {
		case IntentPending:
			counts.PendingIntents++
		case IntentBlocked:
			counts.BlockedIntents++
		case IntentClaimed:
			counts.ClaimedIntents++
		case IntentAmbiguous:
			counts.AmbiguousRecords++
		}
		if intent.Status == IntentBlocked && intent.IssueNumber == 0 && projectHoldReason == "" {
			projectHoldReason = intent.ReasonCode
		}
		if intent.Status != IntentPending && intent.Status != IntentBlocked && intent.Status != IntentClaimed && intent.Status != IntentAmbiguous {
			continue
		}
		queue = append(queue, AutopilotQueueItem{Intent: intent, WaitingReason: waitingReason(intent, profile, projectBreaker, breakerLanes, activeLanes)})
	}
	for _, request := range provisioning {
		if request.Status == ProvisionPending || request.Status == ProvisionClaimed || request.Status == ProvisionSurfaceReady {
			counts.PendingProvisioning++
		}
		if request.Status == ProvisionProvisioned {
			counts.ManagedSessions++
		}
	}
	for _, command := range commands {
		if command.Status == AutonomousMaterializationPending || command.Status == AutonomousMaterializationMaterialized || command.WorkflowStatus == "created" || command.WorkflowStatus == "delivery_pending" || command.WorkflowStatus == "awaiting_result" {
			counts.ActiveCommands++
		}
		if command.Status == AutonomousMaterializationAmbiguous || command.WorkflowStatus == "ambiguous" || command.DeliveryStatus == "uncertain" {
			counts.AmbiguousRecords++
		}
	}
	for _, breaker := range breakers {
		if breaker.Status != BreakerResolved {
			counts.ActiveCircuitBreakers++
		}
	}
	counts.AmbiguousRecords += countAmbiguousResults(ctx, s.store.db, projectID)

	status := AutopilotStatus{
		ProjectID: projectID, Repository: owner + "/" + repository, SyncStatus: syncStatus, SyncError: syncError,
		Profile: profile, Control: control, ActiveWave: activeWave, Intents: intents, Queue: queue,
		Leases: allLeases, ActiveLeases: activeLeases, Provisioning: provisioning, Commands: commands, Results: results,
		CircuitBreakers: breakers, Warnings: warnings, MergeCycles: mergeCycles, Counts: counts,
		ProjectHoldReason: projectHoldReason, LeadBusy: leadBusy, GeneratedAt: s.store.now().UTC(),
	}
	status.NextAction = nextAutopilotAction(status)
	return status, nil
}

func projectLease(value Lease) LeaseProjection {
	return LeaseProjection{
		ID: value.ID, ProjectID: value.ProjectID, LaneKey: value.LaneKey, IntentID: value.IntentID,
		ClaimID: value.ClaimID, LeaseOwner: value.LeaseOwner, Status: value.Status,
		AcquiredAt: value.AcquiredAt, ExpiresAt: value.ExpiresAt, ReleasedAt: value.ReleasedAt,
	}
}

func (s *OperationsService) activeWave(ctx context.Context, projectID int64) (*Wave, error) {
	value, err := scanWave(s.store.db.QueryRowContext(ctx, waveSelect+` WHERE project_id=? AND status IN ('planned','active','waiting','blocked') ORDER BY created_at DESC,wave_id DESC LIMIT 1`, projectID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	issues, err := s.store.waveIssues(ctx, s.store.db, projectID, value.WaveID)
	if err != nil {
		return nil, err
	}
	value.Issues = issues
	return &value, nil
}

func (s *OperationsService) ensureControl(ctx context.Context, projectID int64) (AutopilotControl, error) {
	now := s.store.now().UTC()
	if _, err := s.store.db.ExecContext(ctx, `INSERT INTO autopilot_controls(project_id,revision,last_action,updated_at) VALUES(?,0,'none',?) ON CONFLICT(project_id) DO NOTHING`, projectID, stamp(now)); err != nil {
		return AutopilotControl{}, fmt.Errorf("ensure Autopilot control: %w", err)
	}
	return scanControl(s.store.db.QueryRowContext(ctx, `SELECT project_id,revision,last_action,updated_at FROM autopilot_controls WHERE project_id=?`, projectID))
}

func waitingReason(intent Intent, profile ProjectProfile, projectBreaker bool, breakerLanes, activeLanes map[string]bool) string {
	if intent.Status == IntentBlocked {
		if intent.ReasonCode != "" {
			return intent.ReasonCode
		}
		return "blocked"
	}
	if intent.Status == IntentAmbiguous {
		return "ambiguous evidence requires operator recovery"
	}
	if intent.Status == IntentClaimed {
		return "active lane lease"
	}
	if profile.AutonomyState != AutonomyStateEnabled {
		return "autonomy_" + profile.AutonomyState
	}
	if projectBreaker {
		return "project circuit breaker"
	}
	if breakerLanes[intent.LaneKey] {
		return "lane circuit breaker"
	}
	if activeLanes[intent.LaneKey] {
		return "lane busy"
	}
	return "ready"
}

func nextAutopilotAction(status AutopilotStatus) string {
	if status.Counts.ActiveCircuitBreakers > 0 {
		return "Resolve active circuit breakers before new automatic work."
	}
	switch status.Profile.AutonomyState {
	case AutonomyStateDisabled, AutonomyStateStopped:
		return "Enable Autopilot after verifying the continuous profile and GitHub synchronization."
	case AutonomyStatePaused:
		return "Resume Autopilot when the current durable work may continue."
	}
	if status.ProjectHoldReason != "" {
		return "Resolve the Project hold or owner-required condition."
	}
	if status.Counts.ActiveLeases > 0 || status.Counts.ActiveCommands > 0 || status.Counts.PendingProvisioning > 0 {
		return "Observe current durable work; do not replay or retarget active commands."
	}
	if status.Counts.PendingIntents > 0 {
		return "The scheduler may claim the next eligible Intent."
	}
	if status.ActiveWave != nil {
		return "Complete or verify the active Wave before planning another Wave."
	}
	return "No automatic work is queued. The persistent Lead may plan the next bounded Wave."
}

func (s *OperationsService) listBreakers(ctx context.Context, projectID int64) ([]CircuitBreaker, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,project_id,scope_kind,lane_key,code,reason,recovery_requirements,evidence,expected_head,status,occurrence_count,created_at,updated_at,acknowledged_at,resolved_at FROM autopilot_circuit_breakers WHERE project_id=? ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'acknowledged' THEN 1 ELSE 2 END,updated_at DESC,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]CircuitBreaker, 0)
	for rows.Next() {
		var value CircuitBreaker
		var created, updated, acknowledged, resolved string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.ScopeKind, &value.LaneKey, &value.Code, &value.Reason, &value.RecoveryRequirements, &value.Evidence, &value.ExpectedHead, &value.Status, &value.OccurrenceCount, &created, &updated, &acknowledged, &resolved); err != nil {
			return nil, err
		}
		var parseErr error
		if value.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created); parseErr != nil {
			return nil, parseErr
		}
		if value.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated); parseErr != nil {
			return nil, parseErr
		}
		value.AcknowledgedAt, parseErr = optionalTime(acknowledged)
		if parseErr != nil {
			return nil, parseErr
		}
		value.ResolvedAt, parseErr = optionalTime(resolved)
		if parseErr != nil {
			return nil, parseErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *OperationsService) listProvisioning(ctx context.Context, projectID int64) ([]ProvisioningProjection, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT p.id,p.project_id,p.intent_id,p.lane_lease_id,p.lane_key,p.issue_number,p.role,p.expected_head,p.status,p.completion_reason,p.worker_id,p.worker_session_id,p.tab_id,p.observed_chatgpt_url,p.bound_binding_id,p.bound_binding_version,p.created_at,p.updated_at
		FROM session_provision_requests p
		WHERE p.project_id=? ORDER BY p.created_at DESC,p.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]ProvisioningProjection, 0)
	for rows.Next() {
		var value ProvisioningProjection
		var created, updated string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.IntentID, &value.LeaseID, &value.LaneKey, &value.IssueNumber, &value.Role, &value.ExpectedHead, &value.Status, &value.CompletionReason, &value.WorkerID, &value.WorkerSessionID, &value.TabID, &value.ObservedChatGPTURL, &value.BoundBindingID, &value.BoundBindingVersion, &created, &updated); err != nil {
			return nil, err
		}
		var parseErr error
		if value.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created); parseErr != nil {
			return nil, parseErr
		}
		if value.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated); parseErr != nil {
			return nil, parseErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *OperationsService) listCommands(ctx context.Context, projectID int64) ([]CommandProjection, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT m.project_id,m.id,m.intent_id,m.lease_id,m.provision_request_id,m.scheduler_lane_key,i.issue_number,i.role,COALESCE(p.expected_head,i.expected_head,''),m.status,m.reason_code,m.workflow_command_id,COALESCE(w.status,''),m.delivery_command_id,COALESCE(d.status,''),COALESCE(p.worker_id,''),COALESCE(d.worker_session_id,''),COALESCE(p.tab_id,0),COALESCE(d.binding_id,p.bound_binding_id,''),COALESCE(d.binding_version,p.bound_binding_version,0),COALESCE(p.observed_chatgpt_url,''),m.context_hash,m.prompt_hash,m.updated_at
		FROM autonomous_command_materializations m
		JOIN workflow_intents i ON i.project_id=m.project_id AND i.id=m.intent_id
		LEFT JOIN session_provision_requests p ON p.project_id=m.project_id AND p.id=m.provision_request_id
		LEFT JOIN workflow_commands w ON w.id=m.workflow_command_id
		LEFT JOIN delivery_commands d ON d.id=m.delivery_command_id
		WHERE m.project_id=? ORDER BY m.created_at DESC,m.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]CommandProjection, 0)
	for rows.Next() {
		var value CommandProjection
		var updated string
		if err := rows.Scan(&value.ProjectID, &value.MaterializationID, &value.IntentID, &value.LeaseID, &value.ProvisionRequestID, &value.LaneKey, &value.IssueNumber, &value.Role, &value.ExpectedHead, &value.Status, &value.ReasonCode, &value.WorkflowCommandID, &value.WorkflowStatus, &value.DeliveryCommandID, &value.DeliveryStatus, &value.WorkerID, &value.WorkerSessionID, &value.TabID, &value.BindingID, &value.BindingVersion, &value.ObservedChatGPTURL, &value.ContextHash, &value.PromptHash, &updated); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		value.UpdatedAt = parsed
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *OperationsService) listResults(ctx context.Context, projectID int64) ([]ResultProjection, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT r.project_id,r.github_comment_id,r.issue_number,r.asserted_command_id,r.role,r.result,r.payload_hash,r.validation_status,r.validation_reason,r.accepted_at,r.observed_at
		FROM workflow_results r
		JOIN autonomous_command_materializations m ON m.project_id=r.project_id AND m.workflow_command_id=r.asserted_command_id
		WHERE r.project_id=? ORDER BY r.github_comment_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]ResultProjection, 0)
	for rows.Next() {
		var value ResultProjection
		var accepted, observed string
		if err := rows.Scan(&value.ProjectID, &value.GitHubCommentID, &value.IssueNumber, &value.CommandID, &value.Role, &value.Result, &value.PayloadHash, &value.ValidationStatus, &value.ValidationReason, &accepted, &observed); err != nil {
			return nil, err
		}
		var parseErr error
		value.AcceptedAt, parseErr = optionalTime(accepted)
		if parseErr != nil {
			return nil, parseErr
		}
		if value.ObservedAt, parseErr = time.Parse(time.RFC3339Nano, observed); parseErr != nil {
			return nil, parseErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *OperationsService) listWarnings(ctx context.Context, projectID int64) ([]AutopilotWarning, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT i.id,i.issue_number,i.pr_number,i.expected_head,COALESCE(p.head_sha,''),COALESCE(c.head_sha,''),COALESCE(c.status,''),COALESCE(c.conclusion,'') FROM workflow_intents i LEFT JOIN github_pull_requests p ON p.project_id=i.project_id AND p.pr_number=i.pr_number LEFT JOIN github_ci_summaries c ON c.project_id=p.project_id AND c.pull_request_github_id=p.github_id WHERE i.project_id=? AND i.expected_head<>'' AND i.status IN ('pending','blocked','claimed','ambiguous') ORDER BY i.priority,i.created_at,i.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]AutopilotWarning, 0)
	for rows.Next() {
		var intentID, expected, observed, ciHead, ciStatus, conclusion string
		var issue, pr int
		if err := rows.Scan(&intentID, &issue, &pr, &expected, &observed, &ciHead, &ciStatus, &conclusion); err != nil {
			return nil, err
		}
		if observed != expected {
			values = append(values, AutopilotWarning{Code: BreakerStaleCandidateHead, IntentID: intentID, IssueNumber: issue, PRNumber: pr, ExpectedHead: expected, ObservedHead: observed, Message: "The synchronized PR Head does not match the immutable Intent Head."})
			continue
		}
		if ciHead != expected || ciStatus != "completed" || conclusion != "success" {
			values = append(values, AutopilotWarning{Code: BreakerMissingExactHeadCI, IntentID: intentID, IssueNumber: issue, PRNumber: pr, ExpectedHead: expected, ObservedHead: ciHead, Message: "Conclusive successful CI is missing for the exact Candidate Head."})
		}
	}
	return values, rows.Err()
}

func (s *OperationsService) listMergeCycles(ctx context.Context, projectID int64) ([]MergeCycleProjection, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,project_id,intent_id,issue_number,pr_number,approved_head,observed_merge_commit,status,reason_code,updated_at FROM merge_cycle_readbacks WHERE project_id=? ORDER BY created_at DESC,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]MergeCycleProjection, 0)
	for rows.Next() {
		var value MergeCycleProjection
		var updated string
		if err := rows.Scan(&value.ID, &value.ProjectID, &value.IntentID, &value.IssueNumber, &value.PRNumber, &value.ApprovedHead, &value.ObservedMergeCommit, &value.Status, &value.ReasonCode, &updated); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		value.UpdatedAt = parsed
		values = append(values, value)
	}
	return values, rows.Err()
}

func countAmbiguousResults(ctx context.Context, db *sql.DB, projectID int64) int {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_results WHERE project_id=? AND validation_status='ambiguous'`, projectID).Scan(&count); err != nil {
		return 0
	}
	return count
}

func optionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
