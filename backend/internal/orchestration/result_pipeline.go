package orchestration

import (
	"context"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

type CommandResultReconciler interface {
	ReconcileCommandResult(context.Context, workerloop.Command, workerloop.Result, workerloop.MarkerPayload) error
}

type ResultMaterializationPipeline struct {
	commands CommandResultReconciler
	actions  *Materializer
}

func NewResultMaterializationPipeline(commands CommandResultReconciler, actions *Materializer) *ResultMaterializationPipeline {
	return &ResultMaterializationPipeline{commands: commands, actions: actions}
}

func (p *ResultMaterializationPipeline) ReconcileResult(ctx context.Context, snapshot supervisor.ProjectSnapshot, command workerloop.Command, result workerloop.Result, payload workerloop.MarkerPayload) error {
	if command.ResourceProfile == ContinuousResourceProfile && payload.Version == 2 && p.commands != nil {
		if err := p.commands.ReconcileCommandResult(ctx, command, result, payload); err != nil {
			return err
		}
	}
	if p.actions == nil {
		return nil
	}
	return p.actions.ReconcileResult(ctx, snapshot, command, result, payload)
}
