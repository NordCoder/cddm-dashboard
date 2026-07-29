package delivery

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const commandSelect = `SELECT id,project_id,issue_number,plan_id,plan_hash,context_hash,prompt_hash,prompt_text,action,target_role,lane_key,expected_head,binding_id,binding_version,worker_id,worker_session_id,presence_token,target_kind,target_ref,authority_kind,authority_ref,status,created_at,expires_at,claimed_at,claim_deadline_at,claim_id,claim_request_id,terminal_at,outcome_reason,outcome_evidence FROM delivery_commands`

type rowScanner interface{ Scan(...any) error }

func scanCommand(row rowScanner, command *Command) error {
	var created, expires, claimed, deadline, terminal string
	if err := row.Scan(&command.ID, &command.ProjectID, &command.IssueNumber, &command.PlanID, &command.PlanHash, &command.ContextHash, &command.PromptHash, &command.Prompt, &command.Action, &command.TargetRole, &command.LaneKey, &command.ExpectedHead, &command.BindingID, &command.BindingVersion, &command.WorkerID, &command.WorkerSessionID, &command.PresenceToken, &command.TargetKind, &command.TargetRef, &command.AuthorityKind, &command.AuthorityRef, &command.Status, &created, &expires, &claimed, &deadline, &command.ClaimID, &command.ClaimRequestID, &terminal, &command.OutcomeReason, &command.OutcomeEvidence); err != nil {
		return err
	}
	var err error
	if command.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return fmt.Errorf("parse command created_at: %w", err)
	}
	if command.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires); err != nil {
		return fmt.Errorf("parse command expires_at: %w", err)
	}
	for _, item := range []struct {
		raw   string
		dest  **time.Time
		field string
	}{{claimed, &command.ClaimedAt, "claimed_at"}, {deadline, &command.ClaimDeadlineAt, "claim_deadline_at"}, {terminal, &command.TerminalAt, "terminal_at"}} {
		if item.raw == "" {
			continue
		}
		value, err := time.Parse(time.RFC3339Nano, item.raw)
		if err != nil {
			return fmt.Errorf("parse command %s: %w", item.field, err)
		}
		*item.dest = &value
	}
	return nil
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func transition(ctx context.Context, tx sqlExecutor, id, from, to string, now time.Time, terminalAt, reason, evidence, fallbackReason string) error {
	if reason == "" {
		reason = fallbackReason
	}
	if terminalAt == "" && to != StatusClaimed {
		terminalAt = stamp(now)
	}
	result, err := tx.ExecContext(ctx, "UPDATE delivery_commands SET status=?, terminal_at=?, outcome_reason=?, outcome_evidence=? WHERE id=? AND status=?", to, terminalAt, reason, evidence, id, from)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
