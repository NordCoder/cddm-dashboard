package workerloop

import (
	"context"
	"strings"

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
	externalResults := make(map[int64]workflow.ExternalResult, len(rows))
	for _, result := range rows {
		externalResults[result.GitHubCommentID] = projectResult(snapshot, result)
	}
	externalCommands := make(map[int]workflow.ExternalCommand)
	for _, issue := range snapshot.Issues {
		commands, listErr := s.results.ListCommands(ctx, projectID, issue.Number)
		if listErr != nil {
			return listErr
		}
		command, ok, selectErr := s.currentCommand(ctx, issue, commands)
		if selectErr != nil {
			return selectErr
		}
		if ok {
			externalCommands[issue.Number] = workflow.ExternalCommand{
				ID: command.ID, IssueNumber: command.IssueNumber, Role: command.Role,
				Action: command.Action, ResourceProfile: command.ResourceProfile,
				ContextHash: command.ContextHash, ExpectedHead: command.ExpectedHead,
				Status: command.Status, CreatedAt: command.CreatedAt,
			}
		}
	}
	workflow.SetProjectExternalState(projectID, externalResults, externalCommands)
	return nil
}

func (s *StateService) currentCommand(ctx context.Context, issue supervisor.Issue, commands []Command) (Command, bool, error) {
	currentHead := singleCurrentHead(issue.PullRequests)
	active := make([]Command, 0, 1)
	var latestExceptional *Command
	for index := range commands {
		command := commands[index]
		if activeCommandStatus(command.Status) && command.ExpectedHead != "" && command.ExpectedHead != currentHead {
			updated, err := s.results.SetCommandStatus(ctx, command.ID, CommandSuperseded)
			if err != nil {
				return Command{}, false, err
			}
			command = updated
		}
		if activeCommandStatus(command.Status) {
			active = append(active, command)
		}
		if command.Status == CommandAmbiguous || command.Status == CommandFailed {
			copy := command
			latestExceptional = &copy
		}
	}
	if len(active) > 1 {
		for index := range active {
			updated, err := s.results.SetCommandStatus(ctx, active[index].ID, CommandAmbiguous)
			if err != nil {
				return Command{}, false, err
			}
			active[index] = updated
		}
		return active[len(active)-1], true, nil
	}
	if len(active) == 1 {
		return active[0], true, nil
	}
	if latestExceptional != nil {
		return *latestExceptional, true, nil
	}
	return Command{}, false, nil
}

func activeCommandStatus(status string) bool {
	return status == CommandCreated || status == CommandDeliveryPending || status == CommandAwaitingResult
}

func singleCurrentHead(pullRequests []supervisor.PullRequest) string {
	head := ""
	count := 0
	for _, pull := range pullRequests {
		if !strings.EqualFold(pull.State, "open") {
			continue
		}
		count++
		head = pull.HeadSHA
	}
	if count != 1 {
		return ""
	}
	return head
}
