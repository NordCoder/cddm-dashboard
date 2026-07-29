package orchestration

import (
	"context"
	"database/sql"
	"errors"

	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func (e *AutopilotEngine) ReconcileCommandResult(ctx context.Context, command workerloop.Command, result workerloop.Result, payload workerloop.MarkerPayload) error {
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
	// M12-C1 owns only dispatch/correction completion. Typed merge and
	// next-Wave results are owned by MergeAutopilot, including their blocked and
	// ambiguous terminal states.
	if intent.ActionType != ActionDispatch && intent.ActionType != ActionCorrect {
		return nil
	}
	if value.Status == AutonomousMaterializationCompleted {
		return nil
	}
	if value.Status != AutonomousMaterializationMaterialized {
		return ErrConflict
	}
	if result.ValidationStatus != workerloop.ValidationAccepted {
		_, err := e.store.db.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='ambiguous',reason_code=?,updated_at=? WHERE id=? AND status='materialized'`, result.ValidationReason, stamp(e.now()), value.ID)
		return err
	}
	lease, err := e.scheduler.Lease(ctx, value.ProjectID, value.LeaseID)
	if err != nil {
		return err
	}
	if _, err := e.scheduler.Transition(ctx, LeaseTransition{
		ProjectID: value.ProjectID, LeaseID: lease.ID, LeaseOwner: lease.LeaseOwner,
		LeaseToken: lease.LeaseToken, Target: LeaseCompleted,
	}); err != nil && !errors.Is(err, ErrConflict) {
		return err
	}
	now := e.now().UTC()
	updated, err := e.store.db.ExecContext(ctx, `UPDATE autonomous_command_materializations SET status='completed',reason_code='worker_result_accepted',updated_at=?,completed_at=? WHERE id=? AND status='materialized'`, stamp(now), stamp(now), value.ID)
	if err != nil {
		return err
	}
	count, _ := updated.RowsAffected()
	if count == 1 {
		return nil
	}
	current, readErr := scanAutonomousMaterialization(e.store.db.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE id=?`, value.ID))
	if readErr == nil && current.Status == AutonomousMaterializationCompleted {
		return nil
	}
	if errors.Is(readErr, sql.ErrNoRows) {
		return ErrNotFound
	}
	return ErrConflict
}
