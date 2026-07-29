package orchestration_test

import (
	"context"

	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
)

func (mergePlannerStub) GenerateAutonomous(context.Context, int64, int, string, string) (planning.GenerationResult, error) {
	return planning.GenerationResult{}, nil
}
