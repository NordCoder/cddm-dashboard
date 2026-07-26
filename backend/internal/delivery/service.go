package delivery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
)

var (
	ErrConflict    = errors.New("delivery confirmation conflicts with current authority")
	ErrUnavailable = errors.New("delivery is unavailable")
	ErrNotFound    = errors.New("delivery command not found")
)

type PlanningReader interface {
	Get(context.Context, int64, int, int64) (planning.GenerationResult, error)
	ContextSummary(context.Context, int64, int) (planning.ContextSummary, error)
}

type Config struct {
	PendingTTL, ClaimTTL time.Duration
	Enabled              bool
	Now                  func() time.Time
}
type Service struct {
	db                   *sql.DB
	planning             PlanningReader
	bindings             BindingResolver
	pendingTTL, claimTTL time.Duration
	enabled              bool
	now                  func() time.Time
}

func (s *Service) List(ctx context.Context, projectID int64, issueNumber int) ([]Command, error) {
	rows, err := s.db.QueryContext(ctx, commandSelect+" WHERE project_id=? AND issue_number=? ORDER BY created_at DESC", projectID, issueNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := make([]Command, 0)
	for rows.Next() {
		var command Command
		if err := scanCommand(rows, &command); err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

func New(db *sql.DB, plans PlanningReader, bindings BindingResolver, cfg Config) *Service {
	if cfg.PendingTTL <= 0 {
		cfg.PendingTTL = 5 * time.Minute
	}
	if cfg.ClaimTTL <= 0 {
		cfg.ClaimTTL = time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{db: db, planning: plans, bindings: bindings, pendingTTL: cfg.PendingTTL, claimTTL: cfg.ClaimTTL, enabled: cfg.Enabled, now: cfg.Now}
}

func (s *Service) Create(ctx context.Context, projectID int64, issueNumber int, in Confirmation) (Command, error) {
	if !s.enabled {
		return Command{}, ErrUnavailable
	}
	if projectID <= 0 || issueNumber <= 0 || in.PlanID <= 0 || blank(in.IdempotencyKey) {
		return Command{}, fmt.Errorf("plan_id and idempotency_key are required")
	}
	fingerprint := confirmationFingerprint(in)
	conn, err := s.beginImmediate(ctx)
	if err != nil {
		return Command{}, err
	}
	defer conn.Close()
	defer rollbackImmediate(ctx, conn)
	var existing Command
	err = scanCommand(conn.QueryRowContext(ctx, commandSelect+" WHERE project_id = ? AND issue_number = ? AND idempotency_key = ?", projectID, issueNumber, in.IdempotencyKey), &existing)
	if err == nil {
		if existingFingerprint(existing) != fingerprint {
			return Command{}, ErrConflict
		}
		if err := commitImmediate(ctx, conn); err != nil {
			return Command{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Command{}, fmt.Errorf("read idempotency command: %w", err)
	}
	command, err := s.authorize(ctx, projectID, issueNumber, in)
	if err != nil {
		return Command{}, err
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO delivery_commands (id,project_id,issue_number,plan_id,idempotency_key,confirmation_fingerprint,plan_hash,context_hash,prompt_hash,prompt_text,action,target_role,lane_key,expected_head,binding_id,binding_version,worker_id,worker_session_id,presence_token,target_kind,target_ref,status,created_at,expires_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, command.ID, command.ProjectID, command.IssueNumber, command.PlanID, in.IdempotencyKey, fingerprint, command.PlanHash, command.ContextHash, command.PromptHash, command.Prompt, command.Action, command.TargetRole, command.LaneKey, command.ExpectedHead, command.BindingID, command.BindingVersion, command.WorkerID, command.WorkerSessionID, command.PresenceToken, command.TargetKind, command.TargetRef, command.Status, stamp(command.CreatedAt), stamp(command.ExpiresAt)); err != nil {
		return Command{}, fmt.Errorf("persist delivery command: %w", err)
	}
	if err = commitImmediate(ctx, conn); err != nil {
		return Command{}, fmt.Errorf("commit delivery command: %w", err)
	}
	return command, nil
}

func (s *Service) authorize(ctx context.Context, projectID int64, issueNumber int, in Confirmation) (Command, error) {
	result, err := s.planning.Get(ctx, projectID, issueNumber, in.PlanID)
	if err != nil {
		return Command{}, err
	}
	if result.Plan == nil || !validPlan(result) {
		return Command{}, ErrConflict
	}
	contextValue, err := s.planning.ContextSummary(ctx, projectID, issueNumber)
	if err != nil {
		return Command{}, err
	}
	planHash := result.PolicyDecision.PlanHash
	if planHash == "" {
		return Command{}, ErrConflict
	}
	plan := result.Plan
	if in.ExpectedPlanHash != planHash || in.ExpectedContextHash != contextValue.ContextHash || in.ExpectedHead != contextValue.CurrentHead || in.ExpectedLaneKey != contextValue.Route.LaneKey || contextValue.Route.Action != "dispatch" || plan.Action != contextValue.Route.Action || plan.TargetRole != contextValue.Route.TargetRole || plan.LaneKey == "" || plan.LaneKey != contextValue.Route.LaneKey || plan.ExpectedHead != contextValue.CurrentHead || plan.Source.ContextHash != contextValue.ContextHash {
		return Command{}, ErrConflict
	}
	binding, err := s.resolve(ctx, projectID, plan.LaneKey)
	if err != nil {
		return Command{}, err
	}
	if binding.BindingID != in.ExpectedBindingID || binding.BindingVersion != in.ExpectedBindingVer || binding.PresenceToken != in.ExpectedPresenceToken {
		return Command{}, ErrConflict
	}
	now := s.now().UTC()
	return Command{ID: opaqueID(), ProjectID: projectID, IssueNumber: issueNumber, PlanID: in.PlanID, PlanHash: planHash, ContextHash: contextValue.ContextHash, PromptHash: hash(plan.Prompt), Prompt: plan.Prompt, Action: plan.Action, TargetRole: plan.TargetRole, LaneKey: plan.LaneKey, ExpectedHead: contextValue.CurrentHead, BindingID: binding.BindingID, BindingVersion: binding.BindingVersion, WorkerID: binding.WorkerID, WorkerSessionID: binding.WorkerSessionID, PresenceToken: binding.PresenceToken, TargetKind: binding.TargetKind, TargetRef: binding.TargetRef, Status: StatusPending, CreatedAt: now, ExpiresAt: now.Add(s.pendingTTL)}, nil
}

func (s *Service) ClaimNext(ctx context.Context, in ClaimRequest) (*Execution, error) {
	if !s.enabled {
		return nil, ErrUnavailable
	}
	if blank(in.WorkerID) || blank(in.WorkerSessionID) || blank(in.ClaimRequestID) {
		return nil, fmt.Errorf("worker_id, worker_session_id and claim_request_id are required")
	}
	var command Command
	err := scanCommand(s.db.QueryRowContext(ctx, commandSelect+" WHERE worker_id=? AND claim_request_id=?", in.WorkerID, in.ClaimRequestID), &command)
	if err == nil {
		if command.WorkerSessionID != in.WorkerSessionID {
			return nil, ErrConflict
		}
		if command.Status == StatusClaimed {
			return &Execution{Command: command, ClaimID: command.ClaimID, Prompt: command.Prompt}, nil
		}
		return nil, ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, commandSelect+" WHERE worker_id=? AND status='pending' ORDER BY created_at", in.WorkerID)
	if err != nil {
		return nil, err
	}
	candidates := make([]Command, 0)
	for rows.Next() {
		var candidate Command
		if err := scanCommand(rows, &candidate); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, candidate := range candidates {
		conn, err := s.beginImmediate(ctx)
		if err != nil {
			return nil, err
		}
		committed := false
		claimCommitted := false
		duplicateClaimRequest := false
		func() {
			defer conn.Close()
			defer func() {
				if !committed {
					rollbackImmediate(ctx, conn)
				}
			}()
			var current Command
			if err = scanCommand(conn.QueryRowContext(ctx, commandSelect+" WHERE id=?", candidate.ID), &current); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					err = nil
				}
				return
			}
			if current.Status != StatusPending {
				return
			}
			candidate = current
			now := s.now().UTC()
			if !now.Before(candidate.ExpiresAt) {
				err = transition(ctx, conn, candidate.ID, StatusPending, StatusExpired, now, "", "", "", "expired")
				if err == nil {
					err = commitImmediate(ctx, conn)
					committed = err == nil
				}
				return
			}
			if checkErr := s.current(ctx, candidate, in.WorkerSessionID); checkErr != nil {
				if !errors.Is(checkErr, ErrConflict) && !errors.Is(checkErr, ErrUnavailable) {
					err = checkErr
					return
				}
				err = transition(ctx, conn, candidate.ID, StatusPending, StatusInvalidated, now, "", "", "", "stale_authority")
				if err == nil {
					err = commitImmediate(ctx, conn)
					committed = err == nil
				}
				return
			}
			candidate.Status = StatusClaimed
			candidate.ClaimID = opaqueID()
			candidate.ClaimRequestID = in.ClaimRequestID
			claimedAt := now
			deadline := now.Add(s.claimTTL)
			candidate.ClaimedAt = &claimedAt
			candidate.ClaimDeadlineAt = &deadline
			result, updateErr := conn.ExecContext(ctx, "UPDATE delivery_commands SET status='claimed',claimed_at=?,claim_deadline_at=?,claim_id=?,claim_request_id=? WHERE id=? AND status='pending'", stamp(claimedAt), stamp(deadline), candidate.ClaimID, candidate.ClaimRequestID, candidate.ID)
			err = updateErr
			if err != nil {
				duplicateClaimRequest = isUniqueConstraint(err)
				return
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return
			}
			err = commitImmediate(ctx, conn)
			committed = err == nil
			claimCommitted = committed
		}()
		if duplicateClaimRequest {
			var existing Command
			lookupErr := scanCommand(s.db.QueryRowContext(ctx, commandSelect+" WHERE worker_id=? AND claim_request_id=?", in.WorkerID, in.ClaimRequestID), &existing)
			if lookupErr != nil {
				return nil, err
			}
			if existing.WorkerSessionID != in.WorkerSessionID || existing.Status != StatusClaimed {
				return nil, ErrConflict
			}
			return &Execution{Command: existing, ClaimID: existing.ClaimID, Prompt: existing.Prompt}, nil
		}
		if err != nil {
			return nil, err
		}
		if !claimCommitted {
			continue
		}
		return &Execution{Command: candidate, ClaimID: candidate.ClaimID, Prompt: candidate.Prompt}, nil
	}
	return nil, nil
}

func (s *Service) beginImmediate(ctx context.Context) (*sql.Conn, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func commitImmediate(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, "COMMIT")
	return err
}
func rollbackImmediate(ctx context.Context, conn *sql.Conn) { _, _ = conn.ExecContext(ctx, "ROLLBACK") }

func isUniqueConstraint(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func (s *Service) invalidate(ctx context.Context, id, status string, now time.Time, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := transition(ctx, tx, id, StatusPending, status, now, "", "", "", reason); err != nil && !errors.Is(err, ErrConflict) {
		return err
	}
	return tx.Commit()
}

func (s *Service) Complete(ctx context.Context, in Completion) (Command, error) {
	if !oneOf(in.Outcome, StatusDelivered, StatusFailed, StatusUncertain) || blank(in.CommandID) || blank(in.ClaimID) {
		return Command{}, fmt.Errorf("command_id, claim_id and valid outcome are required")
	}
	if len(in.Reason) > 256 || len(in.Evidence) > 4096 {
		return Command{}, fmt.Errorf("completion reason or evidence exceeds its bounded size")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Command{}, err
	}
	defer tx.Rollback()
	var command Command
	if err = scanCommand(tx.QueryRowContext(ctx, commandSelect+" WHERE id=?", in.CommandID), &command); errors.Is(err, sql.ErrNoRows) {
		return Command{}, ErrNotFound
	} else if err != nil {
		return Command{}, err
	}
	if command.ClaimID != in.ClaimID {
		return Command{}, ErrConflict
	}
	if command.Status == in.Outcome && command.OutcomeReason == in.Reason && command.OutcomeEvidence == in.Evidence {
		return command, tx.Commit()
	}
	if command.Status != StatusClaimed {
		return Command{}, ErrConflict
	}
	now := s.now().UTC()
	if command.ClaimDeadlineAt != nil && !now.Before(*command.ClaimDeadlineAt) {
		if err = transition(ctx, tx, command.ID, StatusClaimed, StatusUncertain, now, "", "", "", "claim_ack_deadline"); err != nil {
			return Command{}, err
		}
		if err = tx.Commit(); err != nil {
			return Command{}, err
		}
		return Command{}, ErrConflict
	}
	if err = transition(ctx, tx, command.ID, StatusClaimed, in.Outcome, now, stamp(now), in.Reason, in.Evidence, ""); err != nil {
		return Command{}, err
	}
	command.Status = in.Outcome
	command.TerminalAt = &now
	command.OutcomeReason = in.Reason
	command.OutcomeEvidence = in.Evidence
	if err = tx.Commit(); err != nil {
		return Command{}, err
	}
	return command, nil
}

func (s *Service) ReconcilePeriodically(ctx context.Context, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Reconcile(ctx); err != nil && report != nil {
				report(err)
			}
		}
	}
}

func (s *Service) Reconcile(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if _, err = tx.ExecContext(ctx, "UPDATE delivery_commands SET status='expired',terminal_at=?,outcome_reason='expired' WHERE status='pending' AND expires_at <= ?", stamp(now), stamp(now)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE delivery_commands SET status='uncertain',terminal_at=?,outcome_reason='claim_ack_deadline' WHERE status='claimed' AND claim_deadline_at <= ?", stamp(now), stamp(now)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Cancel(ctx context.Context, commandID string) (Command, error) {
	if blank(commandID) {
		return Command{}, fmt.Errorf("command_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Command{}, err
	}
	defer tx.Rollback()
	var command Command
	if err = scanCommand(tx.QueryRowContext(ctx, commandSelect+" WHERE id=?", commandID), &command); errors.Is(err, sql.ErrNoRows) {
		return Command{}, ErrNotFound
	} else if err != nil {
		return Command{}, err
	}
	if command.Status != StatusPending {
		return Command{}, ErrConflict
	}
	now := s.now().UTC()
	if err = transition(ctx, tx, commandID, StatusPending, StatusCancelled, now, "", "", "", "cancelled"); err != nil {
		return Command{}, err
	}
	command.Status = StatusCancelled
	command.TerminalAt = &now
	command.OutcomeReason = "cancelled"
	if err = tx.Commit(); err != nil {
		return Command{}, err
	}
	return command, nil
}

func (s *Service) current(ctx context.Context, c Command, session string) error {
	result, err := s.planning.Get(ctx, c.ProjectID, c.IssueNumber, c.PlanID)
	if err != nil {
		return err
	}
	summary, err := s.planning.ContextSummary(ctx, c.ProjectID, c.IssueNumber)
	if err != nil {
		return err
	}
	if result.Plan == nil || !validPlan(result) || result.PolicyDecision.PlanHash != c.PlanHash || result.Plan.Action != c.Action || result.Plan.TargetRole != c.TargetRole || result.Plan.LaneKey != c.LaneKey || result.Plan.ExpectedHead != c.ExpectedHead || result.Plan.Source.ContextHash != c.ContextHash || summary.ContextHash != c.ContextHash || summary.CurrentHead != c.ExpectedHead || summary.Route.Action != c.Action || summary.Route.TargetRole != c.TargetRole || summary.Route.LaneKey != c.LaneKey {
		return ErrConflict
	}
	b, err := s.resolve(ctx, c.ProjectID, c.LaneKey)
	if err != nil {
		return err
	}
	if b.WorkerSessionID != session || b.BindingID != c.BindingID || b.BindingVersion != c.BindingVersion || b.WorkerID != c.WorkerID || b.TargetKind != c.TargetKind || b.TargetRef != c.TargetRef || b.PresenceToken != c.PresenceToken {
		return ErrConflict
	}
	return nil
}
func (s *Service) resolve(ctx context.Context, projectID int64, lane string) (BindingSnapshot, error) {
	if s.bindings == nil {
		return BindingSnapshot{}, ErrUnavailable
	}
	b, err := s.bindings.Resolve(ctx, projectID, lane)
	if err != nil {
		return BindingSnapshot{}, ErrUnavailable
	}
	if !b.Ready || b.LaneKey != lane || blank(b.BindingID) || b.BindingVersion <= 0 || blank(b.WorkerID) || blank(b.WorkerSessionID) || blank(b.TargetKind) || blank(b.TargetRef) || blank(b.PresenceToken) {
		return BindingSnapshot{}, ErrUnavailable
	}
	return b, nil
}
func validPlan(r planning.GenerationResult) bool {
	return (r.Status == planning.StatusApproved || r.Status == planning.StatusFallback) && r.PolicyDecision.Status == planning.StatusApproved
}
func blank(s string) bool { return strings.TrimSpace(s) == "" }
func oneOf(s string, x ...string) bool {
	for _, v := range x {
		if s == v {
			return true
		}
	}
	return false
}
func hash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func opaqueID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func confirmationFingerprint(in Confirmation) string {
	return hash(strings.Join([]string{fmt.Sprint(in.PlanID), in.ExpectedPlanHash, in.ExpectedContextHash, in.ExpectedHead, in.ExpectedLaneKey, in.ExpectedBindingID, fmt.Sprint(in.ExpectedBindingVer), in.ExpectedPresenceToken}, "\x00"))
}
func existingFingerprint(c Command) string {
	return hash(strings.Join([]string{fmt.Sprint(c.PlanID), c.PlanHash, c.ContextHash, c.ExpectedHead, c.LaneKey, c.BindingID, fmt.Sprint(c.BindingVersion), c.PresenceToken}, "\x00"))
}
