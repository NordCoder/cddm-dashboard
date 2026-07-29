package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/NordCoder/cddm-dashboard/backend/internal/planning"
	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

type detailedAutonomousPlanner interface {
	GenerateAutonomousIntent(context.Context, int64, int, string, string, string, ...planning.AutonomousIntentDetail) (planning.GenerationResult, error)
	Get(context.Context, int64, int, int64) (planning.GenerationResult, error)
}

// TypedPlanningAdapter derives immutable PR/base/Wave identity from the one
// active typed Intent. It keeps MergeAutopilot's small planner interface while
// making both first generation and restart reads fail closed on plan drift.
type TypedPlanningAdapter struct {
	store     *Store
	snapshots *supervisor.Store
	planner   detailedAutonomousPlanner
}

func NewTypedPlanningAdapter(store *Store, snapshots *supervisor.Store, planner detailedAutonomousPlanner) (*TypedPlanningAdapter, error) {
	if store == nil || snapshots == nil || planner == nil {
		return nil, fmt.Errorf("typed planning adapter requires orchestration, snapshot and planning services")
	}
	return &TypedPlanningAdapter{store: store, snapshots: snapshots, planner: planner}, nil
}

func (a *TypedPlanningAdapter) GenerateAutonomousIntent(ctx context.Context, projectID int64, issueNumber int, action, role, head string) (planning.GenerationResult, error) {
	intent, detail, err := a.currentIntentDetail(ctx, projectID, issueNumber, action, role, head)
	if err != nil {
		return planning.GenerationResult{}, err
	}
	result, err := a.planner.GenerateAutonomousIntent(ctx, projectID, issueNumber, action, role, head, detail)
	if err != nil {
		return planning.GenerationResult{}, err
	}
	if !typedGenerationMatchesDetail(result, intent, detail) {
		return planning.GenerationResult{}, ErrConflict
	}
	return result, nil
}

func (a *TypedPlanningAdapter) Get(ctx context.Context, projectID int64, issueNumber int, planID int64) (planning.GenerationResult, error) {
	result, err := a.planner.Get(ctx, projectID, issueNumber, planID)
	if err != nil {
		return planning.GenerationResult{}, err
	}
	detail, err := autonomousDetail(result)
	if err != nil {
		return planning.GenerationResult{}, err
	}
	intent, expected, err := a.currentIntentDetail(ctx, projectID, issueNumber, detail.Action, detail.Role, detail.ExpectedHead)
	if err != nil {
		return planning.GenerationResult{}, err
	}
	if !sameAutonomousDetail(detail, expected) || !typedGenerationMatchesDetail(result, intent, expected) {
		return planning.GenerationResult{}, ErrConflict
	}
	return result, nil
}

func (a *TypedPlanningAdapter) currentIntentDetail(ctx context.Context, projectID int64, issueNumber int, action, role, head string) (Intent, planning.AutonomousIntentDetail, error) {
	intents, err := a.store.ListIntents(ctx, projectID, IntentClaimed)
	if err != nil {
		return Intent{}, planning.AutonomousIntentDetail{}, err
	}
	matches := make([]Intent, 0, 1)
	for _, intent := range intents {
		if intent.IssueNumber != issueNumber || intent.ActionType != strings.TrimSpace(action) || intent.Role != strings.TrimSpace(role) {
			continue
		}
		if expected := provisionExpectedHead(intent); strings.TrimSpace(head) != expected {
			continue
		}
		matches = append(matches, intent)
	}
	if len(matches) != 1 {
		return Intent{}, planning.AutonomousIntentDetail{}, ErrConflict
	}
	intent := matches[0]
	detail := planning.AutonomousIntentDetail{
		Action: intent.ActionType, Repository: intent.Repository, Role: intent.Role,
		ExpectedHead: provisionExpectedHead(intent), WaveID: intent.WaveID,
	}
	switch intent.ActionType {
	case ActionMerge:
		snapshot, err := a.snapshots.ProjectSnapshot(ctx, projectID)
		if err != nil {
			return Intent{}, planning.AutonomousIntentDetail{}, err
		}
		baseRef, ok := exactMergeBase(snapshot, intent)
		if !ok {
			return Intent{}, planning.AutonomousIntentDetail{}, ErrConflict
		}
		detail.PRNumber = intent.PRNumber
		detail.ExpectedBaseRef = baseRef
	case ActionPlanNextWave:
		profile, err := a.store.ProjectProfile(ctx, projectID)
		if err != nil {
			return Intent{}, planning.AutonomousIntentDetail{}, err
		}
		if profile.ControlIssueNumber != intent.IssueNumber || intent.WaveID == "" {
			return Intent{}, planning.AutonomousIntentDetail{}, ErrConflict
		}
		detail.ControlIssue = profile.ControlIssueNumber
	default:
		return Intent{}, planning.AutonomousIntentDetail{}, ErrConflict
	}
	return intent, detail, nil
}

func autonomousDetail(result planning.GenerationResult) (planning.AutonomousIntentDetail, error) {
	if result.Plan == nil || result.Plan.Extensions == nil {
		return planning.AutonomousIntentDetail{}, ErrConflict
	}
	raw := result.Plan.Extensions[planning.AutonomousIntentExtension]
	if len(raw) == 0 {
		return planning.AutonomousIntentDetail{}, ErrConflict
	}
	var detail planning.AutonomousIntentDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return planning.AutonomousIntentDetail{}, fmt.Errorf("decode autonomous Intent detail: %w", err)
	}
	return detail, nil
}

func typedGenerationMatchesDetail(result planning.GenerationResult, intent Intent, expected planning.AutonomousIntentDetail) bool {
	if result.Plan == nil || result.PolicyDecision.Status != planning.StatusApproved {
		return false
	}
	detail, err := autonomousDetail(result)
	if err != nil || !sameAutonomousDetail(detail, expected) {
		return false
	}
	if result.Plan.TargetRole != intent.Role || result.Plan.Action != result.Context.Route.Action || result.Plan.LaneKey != result.Context.Route.LaneKey {
		return false
	}
	if result.Plan.ExpectedHead != result.Context.CurrentHead || result.Context.Route.ExpectedHead != result.Context.CurrentHead {
		return false
	}
	if result.Context.Route.Action != "dispatch" || result.Context.Route.TargetRole != intent.Role {
		return false
	}
	return provisionExpectedHead(intent) == result.Context.CurrentHead
}

func sameAutonomousDetail(left, right planning.AutonomousIntentDetail) bool {
	return left.Action == right.Action && strings.EqualFold(left.Repository, right.Repository) &&
		left.Role == right.Role && left.ExpectedHead == right.ExpectedHead && left.PRNumber == right.PRNumber &&
		left.ExpectedBaseRef == right.ExpectedBaseRef && left.WaveID == right.WaveID && left.ControlIssue == right.ControlIssue
}

var _ TypedAutonomousPlanner = (*TypedPlanningAdapter)(nil)
var _ = errors.Is
