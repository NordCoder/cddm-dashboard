// Package orchestration owns durable autonomous-delivery intents, Waves and scheduler-facing records.
package orchestration

import "time"

const (
	AutonomyModeManual     = "manual_owner_dispatch"
	AutonomyModeContinuous = "continuous_dashboard_orchestration"
)

const (
	AutonomyStateDisabled = "disabled"
	AutonomyStateEnabled  = "enabled"
	AutonomyStatePaused   = "paused"
	AutonomyStateStopped  = "stopped"
)

const (
	IntentPending    = "pending"
	IntentBlocked    = "blocked"
	IntentClaimed    = "claimed"
	IntentCompleted  = "completed"
	IntentSuperseded = "superseded"
	IntentRejected   = "rejected"
	IntentAmbiguous  = "ambiguous"
)

const (
	WavePlanned    = "planned"
	WaveActive     = "active"
	WaveWaiting    = "waiting"
	WaveCompleted  = "completed"
	WaveBlocked    = "blocked"
	WaveSuperseded = "superseded"
)

const (
	ActionDispatch     = "dispatch"
	ActionCorrect      = "correct"
	ActionPlanNextWave = "plan_next_wave"
	ActionMerge        = "merge_candidate"
	ActionHold         = "hold"
	ActionOwnerRequired = "owner_required"
)

type Intent struct {
	ID                   string    `json:"intent_id"`
	ProjectID            int64     `json:"project_id"`
	SourceResultCommentID int64     `json:"source_result_comment_id"`
	SourceCommandID      string    `json:"source_command_id"`
	ActionID             string    `json:"action_id"`
	ActionType           string    `json:"action_type"`
	Repository           string    `json:"repository"`
	IssueNumber          int       `json:"issue_number,omitempty"`
	Role                 string    `json:"role,omitempty"`
	PRNumber             int       `json:"pr_number,omitempty"`
	ExpectedHead         string    `json:"expected_head,omitempty"`
	ExpectedPreviousHead string    `json:"expected_previous_head,omitempty"`
	ReasonCode           string    `json:"reason_code,omitempty"`
	DecisionCategory     string    `json:"decision_category,omitempty"`
	WaveID               string    `json:"wave_id,omitempty"`
	Priority             int       `json:"priority"`
	LaneKey              string    `json:"lane_key,omitempty"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type IntentInput struct {
	ID                   string
	ProjectID            int64
	SourceResultCommentID int64
	SourceCommandID      string
	ActionID             string
	ActionType           string
	Repository           string
	IssueNumber          int
	Role                 string
	PRNumber             int
	ExpectedHead         string
	ExpectedPreviousHead string
	ReasonCode           string
	DecisionCategory     string
	WaveID               string
	Priority             int
	LaneKey              string
	Status               string
}

type Wave struct {
	ProjectID         int64     `json:"project_id"`
	WaveID            string    `json:"wave_id"`
	ControlIssueNumber int       `json:"control_issue_number"`
	SourceCommandID   string    `json:"source_command_id"`
	Status            string    `json:"status"`
	Issues            []int     `json:"issues"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type WaveInput struct {
	ProjectID         int64
	WaveID            string
	ControlIssueNumber int
	SourceCommandID   string
	Status            string
	Issues            []int
}
