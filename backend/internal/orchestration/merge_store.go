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
)

const mergeCycleSelect = `SELECT id,project_id,intent_id,workflow_command_id,source_result_comment_id,repository,issue_number,pr_number,approved_head,expected_base_ref,reported_merge_commit,observed_merge_commit,observed_base_ref,status,reason_code,created_at,updated_at,verified_at FROM merge_cycle_readbacks`

func scanMergeCycle(row rowScanner) (MergeCycle, error) {
	var value MergeCycle
	var created, updated, verified string
	if err := row.Scan(
		&value.ID, &value.ProjectID, &value.IntentID, &value.WorkflowCommandID,
		&value.SourceResultCommentID, &value.Repository, &value.IssueNumber, &value.PRNumber,
		&value.ApprovedHead, &value.ExpectedBaseRef, &value.ReportedMergeCommit,
		&value.ObservedMergeCommit, &value.ObservedBaseRef, &value.Status, &value.ReasonCode,
		&created, &updated, &verified,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MergeCycle{}, ErrNotFound
		}
		return MergeCycle{}, fmt.Errorf("scan merge cycle: %w", err)
	}
	var err error
	if value.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return MergeCycle{}, err
	}
	if value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return MergeCycle{}, err
	}
	if verified != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, verified)
		if parseErr != nil {
			return MergeCycle{}, parseErr
		}
		value.VerifiedAt = &parsed
	}
	return value, nil
}

func scanWaveIssue(row rowScanner) (WaveIssue, error) {
	var value WaveIssue
	var completed string
	if err := row.Scan(&value.ProjectID, &value.WaveID, &value.IssueNumber, &value.Position, &value.Status, &value.MergeCommitSHA, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WaveIssue{}, ErrNotFound
		}
		return WaveIssue{}, fmt.Errorf("scan Wave Issue: %w", err)
	}
	if completed != "" {
		parsed, err := time.Parse(time.RFC3339Nano, completed)
		if err != nil {
			return WaveIssue{}, err
		}
		value.CompletedAt = &parsed
	}
	return value, nil
}

func deterministicMergeCycleID(projectID int64, intentID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", projectID, intentID)))
	return "merge-cycle-" + hex.EncodeToString(sum[:16])
}

func mergeRepositoryParts(repository string) (string, string, bool) {
	owner, name, ok := strings.Cut(strings.TrimSpace(repository), "/")
	return owner, name, ok && owner != "" && name != "" && !strings.Contains(name, "/")
}

func issueLifecycleTerminal(state string, labels []string) bool {
	if strings.EqualFold(strings.TrimSpace(state), "closed") {
		return true
	}
	for _, label := range labels {
		value := strings.ToLower(strings.TrimSpace(label))
		value = strings.TrimSpace(strings.TrimPrefix(value, "status:"))
		value = strings.NewReplacer("-", "_", " ", "_").Replace(value)
		for _, prefix := range []string{"status_", "lifecycle_", "stage_"} {
			value = strings.TrimPrefix(value, prefix)
		}
		switch value {
		case "done", "completed", "complete", "closed", "terminal", "merged":
			return true
		}
	}
	return false
}

func mergeCycleTx(ctx context.Context, tx *sql.Tx, id string) (MergeCycle, error) {
	return scanMergeCycle(tx.QueryRowContext(ctx, mergeCycleSelect+` WHERE id=?`, strings.TrimSpace(id)))
}

func mergeCycleByCommandTx(ctx context.Context, tx *sql.Tx, commandID string) (MergeCycle, error) {
	return scanMergeCycle(tx.QueryRowContext(ctx, mergeCycleSelect+` WHERE workflow_command_id=?`, strings.TrimSpace(commandID)))
}

func intentTx(ctx context.Context, tx *sql.Tx, id string) (Intent, error) {
	return scanIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE id=?`, strings.TrimSpace(id)))
}

func waveIssueTx(ctx context.Context, tx *sql.Tx, projectID int64, waveID string, issueNumber int) (WaveIssue, error) {
	return scanWaveIssue(tx.QueryRowContext(ctx, `SELECT project_id,wave_id,issue_number,position,status,merge_commit_sha,completed_at FROM workflow_wave_issues WHERE project_id=? AND wave_id=? AND issue_number=?`, projectID, strings.TrimSpace(waveID), issueNumber))
}
