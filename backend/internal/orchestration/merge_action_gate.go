package orchestration

import (
	"context"
	"errors"

	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

func (e *MergeAutopilotEngine) AllowActionMaterialization(ctx context.Context, command workerloop.Command) (bool, error) {
	if e == nil || command.ID == "" {
		return true, nil
	}
	value, err := scanAutonomousMaterialization(e.store.db.QueryRowContext(ctx, autonomousMaterializationSelect+` WHERE workflow_command_id=?`, command.ID))
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	intent, err := e.store.Intent(ctx, value.IntentID)
	if err != nil {
		return false, err
	}
	return intent.ActionType != ActionMerge, nil
}
