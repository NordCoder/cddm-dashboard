package workerloop

import (
	"context"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

type SyncObserver struct {
	results *Service
	state   *StateService
}

func NewSyncObserver(results *Service, state *StateService) *SyncObserver {
	return &SyncObserver{results: results, state: state}
}

func (o *SyncObserver) ObserveProjectSnapshot(ctx context.Context, snapshot supervisor.ProjectSnapshot) error {
	if err := o.results.ObserveProjectSnapshot(ctx, snapshot); err != nil {
		return err
	}
	return o.state.RefreshProject(ctx, snapshot.Project.ID)
}

func (s *StateService) RefreshProject(ctx context.Context, projectID int64) error {
	rows, err := s.results.ResultsForProject(ctx, projectID)
	if err != nil {
		return err
	}
	snapshot, err := s.snapshots.ProjectSnapshot(ctx, projectID)
	if err != nil {
		return err
	}
	external := make(map[int64]workflow.ExternalResult, len(rows))
	for _, result := range rows {
		external[result.GitHubCommentID] = projectResult(snapshot, result)
	}
	workflow.SetProjectExternalResults(projectID, external)
	return nil
}
