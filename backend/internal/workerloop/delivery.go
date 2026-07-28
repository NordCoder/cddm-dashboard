package workerloop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/delivery"
	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
)

type PlanningReader interface {
	Get(context.Context, int64, int, int64) (planning.GenerationResult, error)
	ContextSummary(context.Context, int64, int) (planning.ContextSummary, error)
}

type DeliveryPlanningAdapter struct {
	base     PlanningReader
	commands *CommandEngine
}

func NewDeliveryPlanningAdapter(base PlanningReader, commands *CommandEngine) *DeliveryPlanningAdapter {
	return &DeliveryPlanningAdapter{base: base, commands: commands}
}

func (a *DeliveryPlanningAdapter) Get(ctx context.Context, projectID int64, issueNumber int, planID int64) (planning.GenerationResult, error) {
	result, err := a.base.Get(ctx, projectID, issueNumber, planID)
	if err != nil {
		return planning.GenerationResult{}, err
	}
	if result.Plan == nil {
		return result, nil
	}
	prepared, err := a.commands.BuildWorkflowCommand(projectID, issueNumber, result)
	if err != nil {
		return planning.GenerationResult{}, err
	}
	copy := result
	plan := *result.Plan
	plan.Prompt = prepared.Prompt
	copy.Plan = &plan
	return copy, nil
}

func (a *DeliveryPlanningAdapter) ContextSummary(ctx context.Context, projectID int64, issueNumber int) (planning.ContextSummary, error) {
	return a.base.ContextSummary(ctx, projectID, issueNumber)
}

type BrowserDelivery interface {
	Create(context.Context, int64, int, delivery.Confirmation) (delivery.Command, error)
	List(context.Context, int64, int) ([]delivery.Command, error)
	ClaimNext(context.Context, delivery.ClaimRequest) (*delivery.Execution, error)
	Complete(context.Context, delivery.Completion) (delivery.Command, error)
	Reconcile(context.Context) error
}

type ProjectStateRefresher interface {
	RefreshProject(context.Context, int64) error
}

type DeliveryCoordinator struct {
	db       *sql.DB
	base     BrowserDelivery
	plans    PlanningReader
	commands *CommandEngine
	state    ProjectStateRefresher
}

func NewDeliveryCoordinator(db *sql.DB, base BrowserDelivery, plans PlanningReader, commands *CommandEngine, state ...ProjectStateRefresher) *DeliveryCoordinator {
	var refresher ProjectStateRefresher
	if len(state) > 0 {
		refresher = state[0]
	}
	return &DeliveryCoordinator{db: db, base: base, plans: plans, commands: commands, state: refresher}
}

func (c *DeliveryCoordinator) Create(ctx context.Context, projectID int64, issueNumber int, confirmation delivery.Confirmation) (delivery.Command, error) {
	browserCommand, err := c.base.Create(ctx, projectID, issueNumber, confirmation)
	if err != nil {
		return delivery.Command{}, err
	}
	generation, err := c.plans.Get(ctx, projectID, issueNumber, confirmation.PlanID)
	if err != nil {
		c.invalidatePendingDelivery(ctx, browserCommand.ID, "workflow_plan_unavailable")
		return delivery.Command{}, err
	}
	prepared, err := c.commands.BuildWorkflowCommand(projectID, issueNumber, generation)
	if err != nil {
		c.invalidatePendingDelivery(ctx, browserCommand.ID, "workflow_prompt_invalid")
		return delivery.Command{}, err
	}
	workflowCommand, err := c.commands.PersistWorkflowCommand(ctx, prepared)
	if err != nil {
		c.invalidatePendingDelivery(ctx, browserCommand.ID, "workflow_command_unavailable")
		return delivery.Command{}, err
	}
	if err := c.link(ctx, workflowCommand.ID, browserCommand.ID); err != nil {
		c.invalidatePendingDelivery(ctx, browserCommand.ID, "workflow_command_already_linked")
		return delivery.Command{}, err
	}
	if err := c.commands.RecordDeliveryOutcome(ctx, workflowCommand.ID, browserCommand.Status); err != nil {
		c.invalidatePendingDelivery(ctx, browserCommand.ID, "workflow_status_failed")
		return delivery.Command{}, err
	}
	if err := c.refreshProject(ctx, projectID); err != nil {
		return delivery.Command{}, err
	}
	return browserCommand, nil
}

func (c *DeliveryCoordinator) List(ctx context.Context, projectID int64, issueNumber int) ([]delivery.Command, error) {
	return c.base.List(ctx, projectID, issueNumber)
}

func (c *DeliveryCoordinator) ClaimNext(ctx context.Context, request delivery.ClaimRequest) (*delivery.Execution, error) {
	return c.base.ClaimNext(ctx, request)
}

func (c *DeliveryCoordinator) Complete(ctx context.Context, completion delivery.Completion) (delivery.Command, error) {
	command, err := c.base.Complete(ctx, completion)
	if err != nil {
		return delivery.Command{}, err
	}
	if err := c.syncCommand(ctx, command); err != nil {
		return delivery.Command{}, err
	}
	if err := c.refreshProject(ctx, command.ProjectID); err != nil {
		return delivery.Command{}, err
	}
	return command, nil
}

type linkedDeliveryStatus struct {
	workflowID string
	projectID  int64
	status     string
}

func (c *DeliveryCoordinator) Reconcile(ctx context.Context) error {
	if err := c.base.Reconcile(ctx); err != nil {
		return err
	}
	rows, err := c.db.QueryContext(ctx, `SELECT l.workflow_command_id,d.project_id,d.status FROM workflow_delivery_links l JOIN delivery_commands d ON d.id=l.delivery_command_id`)
	if err != nil {
		return err
	}
	linked := make([]linkedDeliveryStatus, 0)
	for rows.Next() {
		var value linkedDeliveryStatus
		if err := rows.Scan(&value.workflowID, &value.projectID, &value.status); err != nil {
			rows.Close()
			return err
		}
		linked = append(linked, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	projects := make(map[int64]struct{})
	for _, value := range linked {
		if err := c.commands.RecordDeliveryOutcome(ctx, value.workflowID, value.status); err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
		projects[value.projectID] = struct{}{}
	}
	for projectID := range projects {
		if err := c.refreshProject(ctx, projectID); err != nil {
			return err
		}
	}
	return nil
}

func (c *DeliveryCoordinator) ReconcilePeriodically(ctx context.Context, interval time.Duration, report func(error)) {
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
			if err := c.Reconcile(ctx); err != nil && report != nil {
				report(err)
			}
		}
	}
}

func (c *DeliveryCoordinator) syncCommand(ctx context.Context, command delivery.Command) error {
	var workflowID string
	err := c.db.QueryRowContext(ctx, `SELECT workflow_command_id FROM workflow_delivery_links WHERE delivery_command_id=?`, command.ID).Scan(&workflowID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.commands.RecordDeliveryOutcome(ctx, workflowID, command.Status)
}

func (c *DeliveryCoordinator) link(ctx context.Context, workflowID, deliveryID string) error {
	_, err := c.db.ExecContext(ctx, `INSERT INTO workflow_delivery_links (workflow_command_id,delivery_command_id,created_at) VALUES (?,?,?) ON CONFLICT DO NOTHING`, workflowID, deliveryID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("link workflow command to browser delivery: %w", err)
	}
	linked, err := c.linkedDelivery(ctx, workflowID)
	if err != nil {
		return err
	}
	if linked != deliveryID {
		return ErrConflict
	}
	return nil
}

func (c *DeliveryCoordinator) linkedDelivery(ctx context.Context, workflowID string) (string, error) {
	var deliveryID string
	err := c.db.QueryRowContext(ctx, `SELECT delivery_command_id FROM workflow_delivery_links WHERE workflow_command_id=?`, workflowID).Scan(&deliveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return deliveryID, err
}

func (c *DeliveryCoordinator) invalidatePendingDelivery(ctx context.Context, deliveryID, reason string) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = c.db.ExecContext(ctx, `UPDATE delivery_commands SET status='invalidated',terminal_at=?,outcome_reason=? WHERE id=? AND status='pending'`, now, reason, deliveryID)
}

func (c *DeliveryCoordinator) refreshProject(ctx context.Context, projectID int64) error {
	if c.state == nil {
		return nil
	}
	return c.state.RefreshProject(ctx, projectID)
}
