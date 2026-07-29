package orchestration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func (e *MergeAutopilotEngine) ReconcileCommandResult(ctx context.Context, command workerloop.Command, result workerloop.Result, payload workerloop.MarkerPayload) error {
	if e == nil || command.ID == "" {
		return nil
	}
	value, err := scanAutonomousMaterialization(e.store.db.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE workflow_command_id=?`, command.ID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	intent, err := e.store.Intent(ctx, value.IntentID)
	if err != nil {
		return err
	}
	if intent.ActionType != ActionMerge && intent.ActionType != ActionPlanNextWave {
		return nil
	}
	if value.Status == AutonomousMaterializationCompleted {
		return nil
	}
	if value.Status != AutonomousMaterializationMaterialized {
		return ErrConflict
	}
	if result.ValidationStatus != workerloop.ValidationAccepted {
		reason := strings.TrimSpace(result.ValidationReason)
		if reason == "" {
			reason = "typed_result_invalid"
		}
		return e.markAmbiguous(ctx, command, result, intent, value, reason)
	}
	if payload.CommandID != command.ID || payload.Role != "lead" || command.Role != "lead" || command.ResourceProfile != ContinuousResourceProfile {
		return e.markAmbiguous(ctx, command, result, intent, value, "typed_result_identity_mismatch")
	}

	switch intent.ActionType {
	case ActionMerge:
		switch payload.Result {
		case "merged":
			return e.reconcileMerged(ctx, command, result, payload, intent, value)
		case "hold", "owner_required":
			return e.completeBlocked(ctx, command, result, payload, intent, value)
		default:
			return e.markAmbiguous(ctx, command, result, intent, value, "merge_result_unsupported")
		}
	case ActionPlanNextWave:
		switch payload.Result {
		case "actions_ready":
			outcome, err := e.store.Materialization(ctx, result.ProjectID, result.GitHubCommentID)
			if err != nil {
				return err
			}
			if outcome.Status != MaterializationMaterialized || outcome.SourceCommandID != command.ID || outcome.PayloadHash != result.PayloadHash {
				return e.markAmbiguous(ctx, command, result, intent, value, "next_wave_actions_not_materialized")
			}
			return e.completeTypedCommand(ctx, intent, value, "next_wave_actions_materialized")
		case "hold", "owner_required":
			return e.completeBlocked(ctx, command, result, payload, intent, value)
		default:
			return e.markAmbiguous(ctx, command, result, intent, value, "next_wave_result_unsupported")
		}
	}
	return nil
}

func (e *MergeAutopilotEngine) reconcileMerged(ctx context.Context, command workerloop.Command, result workerloop.Result, payload workerloop.MarkerPayload, intent Intent, value AutonomousMaterialization) error {
	cycle, err := scanMergeCycle(e.store.db.QueryRowContext(ctx, mergeCycleSelect+` WHERE workflow_command_id=?`, command.ID))
	if err != nil {
		return err
	}
	if !strings.EqualFold(payload.Repository, intent.Repository) || payload.Issue != intent.IssueNumber || payload.PR != intent.PRNumber || payload.ApprovedHead != intent.ExpectedHead || payload.MergeCommit == "" {
		return e.markAmbiguous(ctx, command, result, intent, value, "merged_result_identity_mismatch")
	}
	if cycle.Status == MergeCycleVerified {
		if cycle.SourceResultCommentID == result.GitHubCommentID && cycle.ReportedMergeCommit == payload.MergeCommit {
			return nil
		}
		return ErrConflict
	}
	if cycle.Status != MergeCyclePending {
		return ErrConflict
	}
	now := stamp(e.now())
	updated, err := e.store.db.ExecContext(ctx, `UPDATE merge_cycle_readbacks SET source_result_comment_id=?,reported_merge_commit=?,updated_at=?
		WHERE id=? AND status='pending' AND (source_result_comment_id=0 OR source_result_comment_id=?) AND (reported_merge_commit='' OR reported_merge_commit=?)`,
		result.GitHubCommentID, payload.MergeCommit, now, cycle.ID, result.GitHubCommentID, payload.MergeCommit)
	if err != nil {
		return err
	}
	if count, _ := updated.RowsAffected(); count != 1 {
		return e.markAmbiguous(ctx, command, result, intent, value, "merged_result_conflict")
	}

	owner, repository, ok := mergeRepositoryParts(intent.Repository)
	if !ok {
		return e.markAmbiguous(ctx, command, result, intent, value, "merge_repository_invalid")
	}
	facts, err := e.facts.ReadMergeFacts(ctx, owner, repository, intent.IssueNumber, intent.PRNumber)
	if err != nil {
		return err
	}
	reason, pending := verifyMergeFacts(cycle, payload, facts)
	if pending {
		_, err := e.store.db.ExecContext(ctx, `UPDATE merge_cycle_readbacks SET observed_merge_commit=?,observed_base_ref=?,reason_code=?,updated_at=? WHERE id=? AND status='pending'`, facts.MergeCommit, facts.BaseRef, reason, stamp(e.now()), cycle.ID)
		return err
	}
	if reason != "" {
		return e.markAmbiguous(ctx, command, result, intent, value, reason)
	}
	return e.completeVerifiedMerge(ctx, command, result, payload, intent, value, cycle, facts)
}

func verifyMergeFacts(cycle MergeCycle, payload workerloop.MarkerPayload, facts supervisor.MergeFacts) (string, bool) {
	if !strings.EqualFold(facts.Repository, cycle.Repository) || facts.IssueNumber != cycle.IssueNumber || facts.PRNumber != cycle.PRNumber {
		return "merge_readback_target_mismatch", false
	}
	if facts.ApprovedHead != cycle.ApprovedHead || payload.ApprovedHead != cycle.ApprovedHead {
		return "merge_readback_head_mismatch", false
	}
	if strings.TrimSpace(facts.BaseRef) != cycle.ExpectedBaseRef {
		return "merge_readback_base_mismatch", false
	}
	if !facts.Merged || facts.MergedAt == nil || strings.TrimSpace(facts.MergeCommit) == "" {
		return "merge_not_visible", true
	}
	if !strings.EqualFold(facts.PRState, "closed") {
		return "merged_pr_not_closed", true
	}
	if facts.MergeCommit != payload.MergeCommit {
		return "merge_readback_commit_mismatch", false
	}
	if !issueLifecycleTerminal(facts.IssueState, facts.IssueLabels) {
		return "merged_issue_not_terminal", true
	}
	return "", false
}

func (e *MergeAutopilotEngine) completeVerifiedMerge(
	ctx context.Context,
	command workerloop.Command,
	result workerloop.Result,
	payload workerloop.MarkerPayload,
	intent Intent,
	value AutonomousMaterialization,
	cycle MergeCycle,
	facts supervisor.MergeFacts,
) error {
	now := e.now().UTC()
	tx, err := e.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	currentCycle, err := mergeCycleTx(ctx, tx, cycle.ID)
	if err != nil {
		return err
	}
	if currentCycle.Status == MergeCycleVerified {
		return tx.Commit()
	}
	if currentCycle.Status != MergeCyclePending || currentCycle.SourceResultCommentID != result.GitHubCommentID || currentCycle.ReportedMergeCommit != payload.MergeCommit {
		return ErrConflict
	}
	currentIntent, err := intentTx(ctx, tx, intent.ID)
	if err != nil {
		return err
	}
	lease, err := leaseTx(ctx, tx, value.ProjectID, value.LeaseID)
	if err != nil {
		return err
	}
	if currentIntent.Status != IntentClaimed || lease.Status != LeaseActive || lease.IntentID != intent.ID || lease.LeaseOwner != autopilotLeaseOwner {
		return ErrConflict
	}
	materialization, err := scanAutonomousMaterialization(tx.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE id=?`, value.ID))
	if err != nil || materialization.Status != AutonomousMaterializationMaterialized {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE merge_cycle_readbacks SET status='verified',reason_code='merge_readback_verified',observed_merge_commit=?,observed_base_ref=?,updated_at=?,verified_at=? WHERE id=? AND status='pending'`, facts.MergeCommit, facts.BaseRef, stamp(now), stamp(now), cycle.ID); err != nil {
		return err
	}
	if intent.WaveID != "" {
		if err := e.completeWaveIssueTx(ctx, tx, command, result, intent, facts.MergeCommit, now); err != nil {
			return err
		}
	}
	if err := setIntentStatusTx(ctx, tx, intent.ID, IntentClaimed, IntentCompleted, now); err != nil {
		return err
	}
	leaseResult, err := tx.ExecContext(ctx, `UPDATE workflow_lane_leases SET status='completed',released_at=? WHERE id=? AND project_id=? AND status='active'`, stamp(now), lease.ID, lease.ProjectID)
	if err != nil {
		return err
	}
	if count, _ := leaseResult.RowsAffected(); count != 1 {
		return ErrConflict
	}
	materializationResult, err := tx.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='completed',reason_code='merge_readback_verified',updated_at=?,completed_at=? WHERE id=? AND status='materialized'`, stamp(now), stamp(now), value.ID)
	if err != nil {
		return err
	}
	if count, _ := materializationResult.RowsAffected(); count != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

func (e *MergeAutopilotEngine) completeWaveIssueTx(ctx context.Context, tx *sql.Tx, command workerloop.Command, result workerloop.Result, intent Intent, mergeCommit string, now time.Time) error {
	item, err := waveIssueTx(ctx, tx, intent.ProjectID, intent.WaveID, intent.IssueNumber)
	if err != nil {
		return err
	}
	if item.Status == WaveIssueDone {
		if item.MergeCommitSHA != mergeCommit {
			return ErrConflict
		}
	} else {
		updated, err := tx.ExecContext(ctx, `UPDATE workflow_wave_issues SET status='done',merge_commit_sha=?,completed_at=? WHERE project_id=? AND wave_id=? AND issue_number=? AND status IN ('planned','active')`, mergeCommit, stamp(now), intent.ProjectID, intent.WaveID, intent.IssueNumber)
		if err != nil {
			return err
		}
		if count, _ := updated.RowsAffected(); count != 1 {
			return ErrConflict
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_waves SET status='active',updated_at=? WHERE project_id=? AND wave_id=? AND status IN ('planned','waiting')`, stamp(now), intent.ProjectID, intent.WaveID); err != nil {
		return err
	}
	var total, remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),SUM(CASE WHEN status='done' THEN 0 ELSE 1 END) FROM workflow_wave_issues WHERE project_id=? AND wave_id=?`, intent.ProjectID, intent.WaveID).Scan(&total, &remaining); err != nil {
		return err
	}
	if total == 0 || remaining != 0 {
		return nil
	}
	wave, err := scanWave(tx.QueryRowContext(ctx, waveSelect+` WHERE project_id=? AND wave_id=?`, intent.ProjectID, intent.WaveID))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_waves SET status='completed',updated_at=? WHERE project_id=? AND wave_id=?`, stamp(now), intent.ProjectID, intent.WaveID); err != nil {
		return err
	}
	return e.ensureNextWaveIntentTx(ctx, tx, command, result, intent.Repository, wave, now)
}

func (e *MergeAutopilotEngine) ensureNextWaveIntentTx(ctx context.Context, tx *sql.Tx, command workerloop.Command, result workerloop.Result, repository string, wave Wave, now time.Time) error {
	existing, err := scanIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE project_id=? AND wave_id=? AND action_type=?`, wave.ProjectID, wave.WaveID, ActionPlanNextWave))
	if err == nil {
		if existing.IssueNumber != wave.ControlIssueNumber || existing.Role != "lead" || existing.LaneKey != fmt.Sprintf("project:%d:lead", wave.ProjectID) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	actionID := nextWaveActionID(wave.WaveID)
	_, err = e.store.createIntentTx(ctx, tx, IntentInput{
		ID: deterministicIntentID(wave.ProjectID, command.ID, actionID), ProjectID: wave.ProjectID,
		SourceResultCommentID: result.GitHubCommentID, SourceCommandID: command.ID,
		ActionID: actionID, ActionType: ActionPlanNextWave, Repository: repository,
		IssueNumber: wave.ControlIssueNumber, Role: "lead", WaveID: wave.WaveID,
		Priority: 100, LaneKey: fmt.Sprintf("project:%d:lead", wave.ProjectID), Status: IntentPending,
	})
	return err
}

func (e *MergeAutopilotEngine) completeTypedCommand(ctx context.Context, intent Intent, value AutonomousMaterialization, reason string) error {
	now := e.now().UTC()
	tx, err := e.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lease, err := leaseTx(ctx, tx, value.ProjectID, value.LeaseID)
	if err != nil {
		return err
	}
	if err := setIntentStatusTx(ctx, tx, intent.ID, IntentClaimed, IntentCompleted, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_lane_leases SET status='completed',released_at=? WHERE id=? AND status='active'`, stamp(now), lease.ID); err != nil {
		return err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='completed',reason_code=?,updated_at=?,completed_at=? WHERE id=? AND status='materialized'`, reason, stamp(now), stamp(now), value.ID)
	if err != nil {
		return err
	}
	if count, _ := updated.RowsAffected(); count != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

func (e *MergeAutopilotEngine) completeBlocked(ctx context.Context, command workerloop.Command, result workerloop.Result, payload workerloop.MarkerPayload, intent Intent, value AutonomousMaterialization) error {
	reason := strings.TrimSpace(payload.ReasonCode)
	if reason == "" {
		reason = "lead_blocked"
	}
	now := e.now().UTC()
	tx, err := e.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := e.createProjectHoldTx(ctx, tx, command, result, intent, reason); err != nil {
		return err
	}
	if intent.WaveID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_waves SET status='blocked',updated_at=? WHERE project_id=? AND wave_id=?`, stamp(now), intent.ProjectID, intent.WaveID); err != nil {
			return err
		}
	}
	if err := setIntentStatusTx(ctx, tx, intent.ID, IntentClaimed, IntentCompleted, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_lane_leases SET status='completed',released_at=? WHERE id=? AND status='active'`, stamp(now), value.LeaseID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='completed',reason_code=?,updated_at=?,completed_at=? WHERE id=? AND status='materialized'`, reason, stamp(now), stamp(now), value.ID); err != nil {
		return err
	}
	if intent.ActionType == ActionMerge {
		if _, err := tx.ExecContext(ctx, `UPDATE merge_cycle_readbacks SET status='blocked',reason_code=?,source_result_comment_id=?,updated_at=? WHERE intent_id=? AND status='pending'`, reason, result.GitHubCommentID, stamp(now), intent.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (e *MergeAutopilotEngine) markAmbiguous(ctx context.Context, command workerloop.Command, result workerloop.Result, intent Intent, value AutonomousMaterialization, reason string) error {
	reason = normalizeMergeReason(reason)
	now := e.now().UTC()
	tx, err := e.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := e.createProjectHoldTx(ctx, tx, command, result, intent, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='ambiguous',reason_code=?,updated_at=?,completed_at=? WHERE id=? AND status='materialized'`, reason, stamp(now), stamp(now), value.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_intents SET status='ambiguous',updated_at=? WHERE id=? AND status='claimed'`, stamp(now), intent.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_lane_leases SET status='superseded',released_at=? WHERE id=? AND status='active'`, stamp(now), value.LeaseID); err != nil {
		return err
	}
	if intent.WaveID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_waves SET status='blocked',updated_at=? WHERE project_id=? AND wave_id=?`, stamp(now), intent.ProjectID, intent.WaveID); err != nil {
			return err
		}
	}
	if intent.ActionType == ActionMerge {
		if _, err := tx.ExecContext(ctx, `UPDATE merge_cycle_readbacks SET status='ambiguous',reason_code=?,source_result_comment_id=?,updated_at=? WHERE intent_id=? AND status='pending'`, reason, result.GitHubCommentID, stamp(now), intent.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (e *MergeAutopilotEngine) createProjectHoldTx(ctx context.Context, tx *sql.Tx, command workerloop.Command, result workerloop.Result, intent Intent, reason string) error {
	actionID := "merge-readback-hold"
	if intent.ActionType == ActionPlanNextWave {
		actionID = "next-wave-hold"
	}
	_, err := e.store.createIntentTx(ctx, tx, IntentInput{
		ID: deterministicIntentID(intent.ProjectID, command.ID, actionID), ProjectID: intent.ProjectID,
		SourceResultCommentID: result.GitHubCommentID, SourceCommandID: command.ID,
		ActionID: actionID, ActionType: ActionHold, Repository: intent.Repository,
		IssueNumber: 0, ReasonCode: normalizeMergeReason(reason), Priority: 5, Status: IntentBlocked,
	})
	return err
}

func nextWaveActionID(waveID string) string {
	sum := sha256.Sum256([]byte(waveID))
	return "plan-next-wave-" + hex.EncodeToString(sum[:8])
}

func normalizeMergeReason(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(" ", "_", "-", "_").Replace(value)
	if value == "" {
		return "merge_readback_ambiguous"
	}
	if len(value) > 120 {
		value = value[:120]
	}
	return value
}
