package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

type SnapshotStore interface {
	ProjectSnapshot(context.Context, int64) (supervisor.ProjectSnapshot, error)
}

type StateReader interface {
	ProjectWorkflowState(context.Context, int64) (supervisor.ProjectSnapshot, workflow.ProjectState, error)
}

type StateService struct {
	snapshots SnapshotStore
	results   *Store
}

func NewStateService(snapshots SnapshotStore, results *Store) *StateService {
	return &StateService{snapshots: snapshots, results: results}
}

func (s *StateService) ProjectWorkflowState(ctx context.Context, projectID int64) (supervisor.ProjectSnapshot, workflow.ProjectState, error) {
	snapshot, err := s.snapshots.ProjectSnapshot(ctx, projectID)
	if err != nil {
		return supervisor.ProjectSnapshot{}, workflow.ProjectState{}, err
	}
	rows, err := s.results.ResultsForProject(ctx, projectID)
	if err != nil {
		return supervisor.ProjectSnapshot{}, workflow.ProjectState{}, err
	}
	external := make(map[int64]workflow.ExternalResult, len(rows))
	for _, result := range rows {
		external[result.GitHubCommentID] = projectResult(snapshot, result)
	}
	return snapshot, workflow.DeriveProjectWithExternal(snapshot, external), nil
}

func projectResult(snapshot supervisor.ProjectSnapshot, result Result) workflow.ExternalResult {
	projection := workflow.ExternalResult{CommentID: result.GitHubCommentID}
	if result.ValidationStatus != ValidationAccepted {
		projection.TransitionSafe = false
		projection.Warnings = []workflow.Warning{{CommentID: result.GitHubCommentID, Code: "worker_result_" + result.ValidationStatus, Message: result.ValidationReason}}
		if result.ValidationStatus == ValidationMalformed || result.ValidationStatus == ValidationAmbiguous {
			projection.HardError = &workflow.ProtocolError{Code: "worker_result_" + result.ValidationStatus, Message: result.ValidationReason}
		}
		return projection
	}
	var payload MarkerPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		projection.HardError = &workflow.ProtocolError{Code: "worker_result_malformed", Message: "persisted accepted marker is not valid JSON"}
		return projection
	}
	event, warnings := eventFromMarker(snapshot, result.IssueNumber, result.GitHubCommentID, payload)
	projection.Event = event
	projection.Warnings = warnings
	projection.TransitionSafe = event != nil && len(warnings) == 0
	return projection
}

func eventFromMarker(snapshot supervisor.ProjectSnapshot, issueNumber int, commentID int64, payload MarkerPayload) (*workflow.WorkerEvent, []workflow.Warning) {
	event := &workflow.WorkerEvent{Version: 1, Event: "worker_result", Role: payload.Role, Extensions: make(map[string]json.RawMessage)}
	putExtension(event.Extensions, "command_id", payload.CommandID)
	putExtension(event.Extensions, "result", payload.Result)
	putExtension(event.Extensions, "pr", payload.PR)
	putExtension(event.Extensions, "blocking_findings", payload.BlockingFindings)
	putExtension(event.Extensions, "blocker_type", payload.BlockerType)
	putExtension(event.Extensions, "reason_code", payload.ReasonCode)
	putExtension(event.Extensions, "cycle_escalation", payload.CycleEscalation)

	switch payload.Role {
	case "implementor":
		switch payload.Result {
		case "candidate_ready":
			event.Status, event.Head = "completed", payload.Head
			if !candidateMatches(snapshot, issueNumber, payload.PR, payload.Head) {
				return event, []workflow.Warning{{CommentID: commentID, Code: "worker_result_candidate_mismatch", Message: "Implementor Candidate claim does not match the current linked primary PR and exact Head"}}
			}
		case "continue":
			event.Status = "continue"
		case "blocked":
			event.Status = "blocked"
		case "no_op":
			event.Status = "no_op"
		}
	case "qa":
		event.Head = payload.ReviewedHead
		switch payload.Result {
		case "approved":
			event.Status, event.Verdict = "completed", "approved"
		case "changes_required":
			event.Status, event.Verdict = "completed", "changes_required"
		case "blocked_inconclusive":
			event.Status, event.Verdict = "inconclusive", "inconclusive"
		}
	case "lead":
		event.Decision = payload.Result
		switch payload.Result {
		case "dispatch":
			event.Status, event.ResumeRole = "completed", payload.NextRole
		case "continue":
			event.Status, event.ResumeRole = "completed", "lead"
		case "correct":
			event.Status, event.ResumeRole = "completed", "implementor"
		case "ready_to_merge":
			event.Status, event.Head = "completed", payload.ApprovedHead
			putExtension(event.Extensions, "pr", payload.PR)
		case "owner_required":
			event.Status, event.EscalateTo = "blocked", "owner"
		case "hold":
			event.Status = "blocked"
		}
	}
	if len(event.Extensions) == 0 {
		event.Extensions = nil
	}
	return event, nil
}

func candidateMatches(snapshot supervisor.ProjectSnapshot, issueNumber, prNumber int, head string) bool {
	for _, issue := range snapshot.Issues {
		if issue.Number != issueNumber {
			continue
		}
		open := make([]supervisor.PullRequest, 0)
		for _, pr := range issue.PullRequests {
			if pr.State == "open" {
				open = append(open, pr)
			}
		}
		sort.Slice(open, func(i, j int) bool { return open[i].Number < open[j].Number })
		return len(open) == 1 && open[0].Number == prNumber && open[0].HeadSHA == head
	}
	return false
}

func putExtension(target map[string]json.RawMessage, key string, value any) {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return
		}
	case int:
		if typed == 0 {
			return
		}
	case *int:
		if typed == nil {
			return
		}
	}
	encoded, err := json.Marshal(value)
	if err == nil {
		target[key] = encoded
	}
}

func externalString(result workflow.ResultEvidence, key string) string {
	value := result.Extensions[key]
	var decoded string
	if len(value) > 0 {
		_ = json.Unmarshal(value, &decoded)
	}
	return decoded
}

func externalInt(result workflow.ResultEvidence, key string) int {
	value := result.Extensions[key]
	var decoded int
	if len(value) > 0 {
		_ = json.Unmarshal(value, &decoded)
	}
	return decoded
}

func validateProjectedResult(result workflow.ResultEvidence) error {
	if result.CommentID <= 0 || result.Role == "" || result.Status == "" {
		return fmt.Errorf("projected result is incomplete")
	}
	return nil
}
