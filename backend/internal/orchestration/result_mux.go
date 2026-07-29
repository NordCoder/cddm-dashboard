package orchestration

import (
	"context"

	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

type CommandResultMux struct {
	reconcilers []CommandResultReconciler
}

func NewCommandResultMux(reconcilers ...CommandResultReconciler) *CommandResultMux {
	values := make([]CommandResultReconciler, 0, len(reconcilers))
	for _, reconciler := range reconcilers {
		if reconciler != nil {
			values = append(values, reconciler)
		}
	}
	return &CommandResultMux{reconcilers: values}
}

func (m *CommandResultMux) AllowActionMaterialization(ctx context.Context, command workerloop.Command) (bool, error) {
	if m == nil {
		return true, nil
	}
	for _, reconciler := range m.reconcilers {
		gate, ok := reconciler.(ActionMaterializationGate)
		if !ok {
			continue
		}
		allowed, err := gate.AllowActionMaterialization(ctx, command)
		if err != nil || !allowed {
			return allowed, err
		}
	}
	return true, nil
}

func (m *CommandResultMux) ReconcileCommandResult(ctx context.Context, command workerloop.Command, result workerloop.Result, payload workerloop.MarkerPayload) error {
	if m == nil {
		return nil
	}
	for _, reconciler := range m.reconcilers {
		if err := reconciler.ReconcileCommandResult(ctx, command, result, payload); err != nil {
			return err
		}
	}
	return nil
}
