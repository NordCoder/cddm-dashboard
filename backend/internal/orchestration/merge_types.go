package orchestration

import "time"

const (
	MergeCyclePending   = "pending"
	MergeCycleVerified  = "verified"
	MergeCycleBlocked   = "blocked"
	MergeCycleAmbiguous = "ambiguous"
)

const (
	WaveIssuePlanned    = "planned"
	WaveIssueActive     = "active"
	WaveIssueDone       = "done"
	WaveIssueBlocked    = "blocked"
	WaveIssueSuperseded = "superseded"
)

type MergeCycle struct {
	ID                    string     `json:"merge_cycle_id"`
	ProjectID             int64      `json:"project_id"`
	IntentID              string     `json:"intent_id"`
	WorkflowCommandID     string     `json:"workflow_command_id"`
	SourceResultCommentID int64      `json:"source_result_comment_id,omitempty"`
	Repository            string     `json:"repository"`
	IssueNumber           int        `json:"issue_number"`
	PRNumber              int        `json:"pr_number"`
	ApprovedHead          string     `json:"approved_head"`
	ExpectedBaseRef       string     `json:"expected_base_ref"`
	ReportedMergeCommit   string     `json:"reported_merge_commit,omitempty"`
	ObservedMergeCommit   string     `json:"observed_merge_commit,omitempty"`
	ObservedBaseRef       string     `json:"observed_base_ref,omitempty"`
	Status                string     `json:"status"`
	ReasonCode            string     `json:"reason_code,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	VerifiedAt            *time.Time `json:"verified_at,omitempty"`
}

type WaveIssue struct {
	ProjectID      int64      `json:"project_id"`
	WaveID         string     `json:"wave_id"`
	IssueNumber    int        `json:"issue_number"`
	Position       int        `json:"position"`
	Status         string     `json:"status"`
	MergeCommitSHA string     `json:"merge_commit_sha,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}
