package workerloop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

type Service struct {
	store *Store
	now   func() time.Time
}

func NewService(store *Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) ObserveProjectSnapshot(ctx context.Context, snapshot supervisor.ProjectSnapshot) error {
	seen := make([]int64, 0)
	for _, issue := range snapshot.Issues {
		for _, comment := range issue.Comments {
			parsed := ParseMarker(comment.Body)
			if !parsed.Present {
				if err := s.store.DeleteResult(ctx, snapshot.Project.ID, comment.GitHubID); err != nil {
					return err
				}
				continue
			}
			seen = append(seen, comment.GitHubID)
			result := s.correlate(ctx, snapshot.Project.ID, issue.Number, comment, parsed)
			if err := s.persistResult(ctx, result); err != nil {
				return err
			}
		}
	}
	if err := s.store.DeleteMissingResults(ctx, snapshot.Project.ID, seen); err != nil {
		return err
	}
	for _, issue := range snapshot.Issues {
		commands, err := s.store.ListCommands(ctx, snapshot.Project.ID, issue.Number)
		if err != nil {
			return err
		}
		for _, command := range commands {
			if err := s.reconcileCommand(ctx, command); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) persistResult(ctx context.Context, result Result) error {
	existing, err := s.store.ResultByComment(ctx, result.ProjectID, result.GitHubCommentID)
	if err == nil && existing.ValidationStatus == ValidationAccepted && (result.ValidationStatus != ValidationAccepted || existing.PayloadHash != result.PayloadHash) {
		result.ValidationStatus = ValidationAmbiguous
		result.ValidationReason = "accepted_result_mutated"
		result.AcceptedAt = nil
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.store.UpsertResult(ctx, result)
}

func (s *Service) correlate(ctx context.Context, projectID int64, issueNumber int, comment supervisor.Comment, parsed ParsedMarker) Result {
	observedAt := comment.UpdatedAt.UTC()
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	result := Result{
		ProjectID: projectID, GitHubCommentID: comment.GitHubID, IssueNumber: issueNumber,
		CommandID: parsed.Payload.CommandID, Role: parsed.Payload.Role, Result: parsed.Payload.Result,
		Payload: parsed.JSON, PayloadHash: parsed.Hash, ValidationStatus: parsed.Status,
		ValidationReason: parsed.Reason, ObservedAt: observedAt,
	}
	if len(result.Payload) == 0 {
		result.Payload = []byte("{}")
		result.PayloadHash = hashBytes(result.Payload)
	}
	if parsed.Status != ValidationAccepted {
		return result
	}
	command, err := s.store.GetCommand(ctx, parsed.Payload.CommandID)
	if errors.Is(err, ErrNotFound) {
		result.ValidationStatus = ValidationUnbound
		result.ValidationReason = "unknown_command"
		return result
	}
	if err != nil {
		result.ValidationStatus = ValidationUnbound
		result.ValidationReason = "command_lookup_failed"
		return result
	}
	if command.ProjectID != projectID || command.IssueNumber != issueNumber {
		result.ValidationStatus = ValidationUnbound
		result.ValidationReason = "command_scope_mismatch"
		return result
	}
	if command.Role != parsed.Payload.Role {
		result.ValidationStatus = ValidationWrongRole
		result.ValidationReason = "command_role_mismatch"
		return result
	}
	if markerStaleForCommand(command, parsed.Payload) {
		result.ValidationStatus = ValidationStale
		result.ValidationReason = "expected_head_mismatch"
		return result
	}
	acceptedAt := s.now().UTC()
	result.AcceptedAt = &acceptedAt
	return result
}

func (s *Service) reconcileCommand(ctx context.Context, command Command) error {
	if command.Status == CommandAmbiguous || command.Status == CommandSuperseded {
		return nil
	}
	results, err := s.store.ResultsForCommand(ctx, command.ProjectID, command.ID)
	if err != nil {
		return err
	}
	accepted := make([]Result, 0)
	hashes := make(map[string]struct{})
	for _, result := range results {
		if result.ValidationStatus != ValidationAccepted {
			continue
		}
		accepted = append(accepted, result)
		hashes[result.PayloadHash] = struct{}{}
	}
	if len(hashes) > 1 {
		if err := s.store.MarkCommandResultsAmbiguous(ctx, command.ProjectID, command.ID); err != nil {
			return err
		}
		_, err := s.store.SetCommandStatus(ctx, command.ID, CommandAmbiguous)
		return err
	}
	if len(accepted) == 0 {
		if terminalCommandStatus(command.Status) {
			_, err := s.store.SetCommandStatus(ctx, command.ID, CommandAmbiguous)
			return err
		}
		return nil
	}
	desired, err := statusForResult(accepted[0])
	if err != nil {
		return err
	}
	if command.Status == desired {
		return nil
	}
	if terminalCommandStatus(command.Status) {
		_, err := s.store.SetCommandStatus(ctx, command.ID, CommandAmbiguous)
		return err
	}
	_, err = s.store.SetCommandStatus(ctx, command.ID, desired)
	return err
}

func markerStaleForCommand(command Command, payload MarkerPayload) bool {
	if command.ExpectedHead == "" {
		return false
	}
	if payload.Role == "qa" {
		return payload.ReviewedHead != command.ExpectedHead
	}
	if payload.Role == "lead" && (payload.Result == "ready_to_merge" || payload.Result == "merged") {
		return payload.ApprovedHead != command.ExpectedHead
	}
	return false
}

func statusForResult(result Result) (string, error) {
	payload, err := decodeCanonicalMarker(result.Payload)
	if err != nil {
		return "", err
	}
	switch payload.Role {
	case "implementor":
		if payload.Result == "blocked" {
			return CommandBlocked, nil
		}
		return CommandCompleted, nil
	case "qa":
		if payload.Result == "blocked_inconclusive" || payload.Result == "blocked" || payload.Result == "inconclusive" {
			return CommandInconclusive, nil
		}
		return CommandCompleted, nil
	case "lead":
		if payload.Result == "owner_required" || payload.Result == "hold" {
			return CommandBlocked, nil
		}
		return CommandCompleted, nil
	default:
		return "", fmt.Errorf("unsupported accepted role %q", payload.Role)
	}
}
