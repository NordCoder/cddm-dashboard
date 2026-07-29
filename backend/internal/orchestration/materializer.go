package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workerloop"
)

type Materializer struct {
	store *Store
}

func NewMaterializer(store *Store) *Materializer {
	return &Materializer{store: store}
}

// ReconcileResult consumes only command-correlated result evidence supplied by
// workerloop. It never creates Workflow Commands or executable prompts.
func (m *Materializer) ReconcileResult(ctx context.Context, snapshot supervisor.ProjectSnapshot, command workerloop.Command, result workerloop.Result, payload workerloop.MarkerPayload) error {
	if command.ResourceProfile != ContinuousResourceProfile || payload.Version != 2 || payload.Role != "lead" || payload.Result != "actions_ready" {
		return nil
	}
	if result.ValidationStatus != workerloop.ValidationAccepted {
		return m.store.MarkMaterializationAmbiguous(ctx, result.ProjectID, result.GitHubCommentID, result.PayloadHash, result.ValidationReason)
	}

	profile, err := m.store.ProjectProfile(ctx, snapshot.Project.ID)
	if err != nil {
		return err
	}
	outcome := MaterializationInput{
		ProjectID: snapshot.Project.ID, SourceResultCommentID: result.GitHubCommentID,
		SourceCommandID: command.ID, PayloadHash: result.PayloadHash,
	}
	if profile.AutonomyMode != AutonomyModeContinuous {
		outcome.Status, outcome.ReasonCode = MaterializationSkipped, "manual_owner_dispatch"
		_, err := m.store.RecordMaterialization(ctx, outcome)
		return err
	}
	if profile.AutonomyState != AutonomyStateEnabled {
		outcome.Status, outcome.ReasonCode = MaterializationSkipped, "autonomy_"+profile.AutonomyState
		_, err := m.store.RecordMaterialization(ctx, outcome)
		return err
	}
	if command.IssueNumber != profile.ControlIssueNumber {
		return m.block(ctx, outcome, "control_issue_mismatch")
	}
	repository := snapshot.Project.Owner + "/" + snapshot.Project.Repository
	if len(payload.Actions) == 0 {
		return m.block(ctx, outcome, "actions_missing")
	}
	for _, action := range payload.Actions {
		if !strings.EqualFold(action.Repository, repository) {
			return m.block(ctx, outcome, "repository_mismatch")
		}
		if reason := validateActionAgainstSnapshot(action, payload.Wave, profile, snapshot); reason != "" {
			return m.block(ctx, outcome, reason)
		}
	}

	var wave *WaveInput
	waveID := ""
	if payload.Wave != nil {
		waveID = payload.Wave.WaveID
		wave = &WaveInput{
			ProjectID: snapshot.Project.ID, WaveID: payload.Wave.WaveID,
			ControlIssueNumber: payload.Wave.ControlIssue, SourceCommandID: command.ID,
			Status: WavePlanned, Issues: append([]int(nil), payload.Wave.Issues...),
		}
		if wave.ControlIssueNumber != profile.ControlIssueNumber {
			return m.block(ctx, outcome, "wave_control_issue_mismatch")
		}
	}

	intents := make([]IntentInput, 0, len(payload.Actions))
	for _, action := range payload.Actions {
		status := IntentPending
		if action.Type == ActionHold || action.Type == ActionOwnerRequired {
			status = IntentBlocked
		}
		intents = append(intents, IntentInput{
			ID:        deterministicIntentID(snapshot.Project.ID, command.ID, action.ActionID),
			ProjectID: snapshot.Project.ID, SourceResultCommentID: result.GitHubCommentID,
			SourceCommandID: command.ID, ActionID: action.ActionID, ActionType: action.Type,
			Repository: repository, IssueNumber: action.Issue, Role: action.Role, PRNumber: action.PR,
			ExpectedHead: action.ExpectedHead, ExpectedPreviousHead: action.ExpectedPreviousHead,
			ReasonCode: action.ReasonCode, DecisionCategory: action.DecisionCategory,
			WaveID: waveIDForAction(action, payload.Wave, waveID), Priority: actionPriority(action),
			LaneKey: actionLane(snapshot.Project.ID, action), Status: status,
		})
	}
	_, _, err = m.store.MaterializeBatch(ctx, outcome, wave, intents)
	return err
}

func (m *Materializer) block(ctx context.Context, outcome MaterializationInput, reason string) error {
	outcome.Status = MaterializationBlocked
	outcome.ReasonCode = reason
	_, err := m.store.RecordMaterialization(ctx, outcome)
	return err
}

func validateActionAgainstSnapshot(action workerloop.ActionPayload, wave *workerloop.WavePayload, profile ProjectProfile, snapshot supervisor.ProjectSnapshot) string {
	if action.Type == ActionPlanNextWave && action.Issue != profile.ControlIssueNumber {
		return "plan_control_issue_mismatch"
	}
	if action.Issue == 0 {
		if action.Type == ActionHold || action.Type == ActionOwnerRequired {
			return ""
		}
		return "issue_missing"
	}
	issue, ok := snapshotIssue(snapshot, action.Issue)
	if !ok {
		return "issue_not_synchronized"
	}
	switch action.Type {
	case ActionDispatch:
		if action.Role == "qa" && !issueHasHead(issue, action.ExpectedHead) {
			return "qa_head_mismatch"
		}
	case ActionCorrect:
		if action.ExpectedPreviousHead != "" && !issueHasHead(issue, action.ExpectedPreviousHead) {
			return "correction_head_mismatch"
		}
	case ActionMerge:
		if !issueHasPRHead(issue, action.PR, action.ExpectedHead) {
			return "merge_candidate_mismatch"
		}
	}
	if wave != nil && action.Type != ActionPlanNextWave && action.Issue > 0 && !containsIssue(wave.Issues, action.Issue) {
		return "action_outside_wave"
	}
	return ""
}

func snapshotIssue(snapshot supervisor.ProjectSnapshot, number int) (supervisor.Issue, bool) {
	for _, issue := range snapshot.Issues {
		if issue.Number == number {
			return issue, true
		}
	}
	return supervisor.Issue{}, false
}

func issueHasHead(issue supervisor.Issue, head string) bool {
	if head == "" {
		return true
	}
	for _, pr := range issue.PullRequests {
		if pr.HeadSHA == head {
			return true
		}
	}
	return false
}

func issueHasPRHead(issue supervisor.Issue, prNumber int, head string) bool {
	for _, pr := range issue.PullRequests {
		if pr.Number == prNumber && pr.HeadSHA == head {
			return true
		}
	}
	return false
}

func containsIssue(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func deterministicIntentID(projectID int64, commandID, actionID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", projectID, commandID, actionID)))
	return "intent-" + hex.EncodeToString(sum[:16])
}

func actionPriority(action workerloop.ActionPayload) int {
	switch action.Type {
	case ActionCorrect:
		return 10
	case ActionMerge:
		return 20
	case ActionDispatch:
		if action.Role == "qa" {
			return 30
		}
		if action.Role == "lead" {
			return 40
		}
		return 50
	case ActionHold, ActionOwnerRequired:
		return 5
	case ActionPlanNextWave:
		return 100
	default:
		return 1000
	}
}

func actionLane(projectID int64, action workerloop.ActionPayload) string {
	switch action.Type {
	case ActionMerge, ActionPlanNextWave:
		return fmt.Sprintf("project:%d:lead", projectID)
	case ActionCorrect:
		return fmt.Sprintf("project:%d:issue:%d:implementor", projectID, action.Issue)
	case ActionDispatch:
		if action.Role == "qa" {
			return fmt.Sprintf("project:%d:issue:%d:qa:%s", projectID, action.Issue, action.ExpectedHead)
		}
		if action.Role == "lead" {
			return fmt.Sprintf("project:%d:lead", projectID)
		}
		return fmt.Sprintf("project:%d:issue:%d:implementor", projectID, action.Issue)
	default:
		return ""
	}
}

func waveIDForAction(action workerloop.ActionPayload, wave *workerloop.WavePayload, waveID string) string {
	if wave == nil || action.Type == ActionPlanNextWave || action.Issue == 0 {
		return ""
	}
	return waveID
}
