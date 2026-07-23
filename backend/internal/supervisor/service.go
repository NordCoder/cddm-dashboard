package supervisor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type GitHubClient interface {
	Snapshot(ctx context.Context, owner, repository string) (RepositorySnapshot, error)
}

type Service struct {
	store          *Store
	client         GitHubClient
	syncTimeout    time.Duration
	maxConcurrency int
}

type SyncResult struct {
	ProjectID int64
	Error     error
}

func NewService(store *Store, client GitHubClient, syncTimeout time.Duration, maxConcurrency int) *Service {
	if syncTimeout <= 0 {
		syncTimeout = 2 * time.Minute
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}
	return &Service{
		store:          store,
		client:         client,
		syncTimeout:    syncTimeout,
		maxConcurrency: maxConcurrency,
	}
}

func (s *Service) SyncProject(ctx context.Context, projectID int64) (ProjectSnapshot, error) {
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	if err := s.store.MarkSyncStarted(ctx, projectID); err != nil {
		return ProjectSnapshot{}, err
	}

	syncContext, cancel := context.WithTimeout(ctx, s.syncTimeout)
	defer cancel()
	snapshot, err := s.client.Snapshot(syncContext, project.Owner, project.Repository)
	if err != nil {
		wrapped := fmt.Errorf("sync %s/%s: %w", project.Owner, project.Repository, err)
		if markErr := s.store.MarkSyncFailed(context.WithoutCancel(ctx), projectID, wrapped); markErr != nil {
			return ProjectSnapshot{}, fmt.Errorf("%v; record failure: %w", wrapped, markErr)
		}
		return ProjectSnapshot{}, wrapped
	}
	if snapshot.FetchedAt.IsZero() {
		snapshot.FetchedAt = time.Now().UTC()
	}
	if err := s.store.ReplaceSnapshot(syncContext, projectID, snapshot); err != nil {
		wrapped := fmt.Errorf("store %s/%s snapshot: %w", project.Owner, project.Repository, err)
		if markErr := s.store.MarkSyncFailed(context.WithoutCancel(ctx), projectID, wrapped); markErr != nil {
			return ProjectSnapshot{}, fmt.Errorf("%v; record failure: %w", wrapped, markErr)
		}
		return ProjectSnapshot{}, wrapped
	}
	return s.store.ProjectSnapshot(ctx, projectID)
}

func (s *Service) SyncProjects(ctx context.Context, projectIDs []int64) []SyncResult {
	results := make([]SyncResult, len(projectIDs))
	semaphore := make(chan struct{}, s.maxConcurrency)
	var group sync.WaitGroup
	for index, projectID := range projectIDs {
		index, projectID := index, projectID
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[index] = SyncResult{ProjectID: projectID, Error: ctx.Err()}
				return
			}
			_, err := s.SyncProject(ctx, projectID)
			results[index] = SyncResult{ProjectID: projectID, Error: err}
		}()
	}
	group.Wait()
	return results
}

type Poller struct {
	store        *Store
	service      *Service
	scanInterval time.Duration
	now          func() time.Time
}

func NewPoller(store *Store, service *Service, scanInterval time.Duration) *Poller {
	if scanInterval <= 0 {
		scanInterval = 15 * time.Second
	}
	return &Poller{
		store:        store,
		service:      service,
		scanInterval: scanInterval,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (p *Poller) Run(ctx context.Context) {
	p.tick(ctx)
	ticker := time.NewTicker(p.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) []SyncResult {
	projects, err := p.store.ListProjects(ctx)
	if err != nil {
		return []SyncResult{{Error: err}}
	}
	now := p.now()
	projectIDs := make([]int64, 0, len(projects))
	for _, project := range projects {
		if !project.PollingEnabled {
			continue
		}
		if project.SyncStatus == "syncing" && project.LastSyncStartedAt != nil &&
			now.Sub(*project.LastSyncStartedAt) < p.service.syncTimeout {
			continue
		}
		lastAttempt := project.LastSyncCompletedAt
		if lastAttempt == nil {
			lastAttempt = project.LastSyncStartedAt
		}
		if lastAttempt == nil || now.Sub(*lastAttempt) >= time.Duration(project.PollIntervalSeconds)*time.Second {
			projectIDs = append(projectIDs, project.ID)
		}
	}
	if len(projectIDs) == 0 {
		return nil
	}
	return p.service.SyncProjects(ctx, projectIDs)
}
