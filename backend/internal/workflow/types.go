package workflow

import (
	"encoding/json"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

type ParseLevel string

const (
	ParseLevelEnvelope   ParseLevel = "authoritative"
	ParseLevelHeading    ParseLevel = "legacy_heading"
	ParseLevelActivity   ParseLevel = "unclassified"
	ParseLevelNoActivity ParseLevel = "empty"
)

type AttentionKind string

const (
	AttentionNormal          AttentionKind = "normal"
	AttentionWaiting         AttentionKind = "waiting"
	AttentionActionRequired  AttentionKind = "action_required"
	AttentionCIFailed        AttentionKind = "ci_failed"
	AttentionBlocked         AttentionKind = "blocked"
	AttentionOwnerRequired   AttentionKind = "owner_required"
	AttentionQAInvalidated   AttentionKind = "qa_invalidated"
	AttentionAmbiguous       AttentionKind = "ambiguous"
	AttentionProtocolWarning AttentionKind = "protocol_warning"
	AttentionTerminal        AttentionKind = "terminal"
)

type Warning struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	CommentID int64  `json:"comment_id,omitempty"`
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type WorkerEvent struct {
	Version    int                        `json:"v"`
	Event      string                     `json:"event"`
	Role       string                     `json:"role"`
	Status     string                     `json:"status"`
	Head       string                     `json:"head,omitempty"`
	Verdict    string                     `json:"verdict,omitempty"`
	Decision   string                     `json:"decision,omitempty"`
	ResumeRole string                     `json:"resume_role,omitempty"`
	Resolves   json.RawMessage            `json:"resolves,omitempty"`
	EscalateTo string                     `json:"escalate_to,omitempty"`
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
}

type ParsedComment struct {
	ProjectID      int64          `json:"project_id"`
	IssueNumber    int            `json:"issue_number"`
	CommentID      int64          `json:"comment_id"`
	Author         string         `json:"author"`
	URL            string         `json:"url"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	Level          ParseLevel     `json:"level"`
	Heading        string         `json:"heading,omitempty"`
	Markdown       string         `json:"markdown"`
	Meaningful     bool           `json:"meaningful"`
	TransitionSafe bool           `json:"transition_safe"`
	Event          *WorkerEvent   `json:"event,omitempty"`
	Warnings       []Warning      `json:"warnings"`
	HardError      *ProtocolError `json:"hard_error,omitempty"`
}

type ProjectIdentity struct {
	ID           int64  `json:"id"`
	Owner        string `json:"owner"`
	Repository   string `json:"repository"`
	WorkflowMode string `json:"workflow_mode"`
}

type WorkUnitIdentity struct {
	ProjectID     int64  `json:"project_id"`
	Owner         string `json:"owner"`
	Repository    string `json:"repository"`
	IssueGitHubID int64  `json:"issue_github_id"`
	IssueNumber   int    `json:"issue_number"`
	Title         string `json:"title"`
	URL           string `json:"url"`
}

type Candidate struct {
	GitHubID       int64                `json:"github_id"`
	Number         int                  `json:"number"`
	Title          string               `json:"title"`
	Draft          bool                 `json:"draft"`
	MergeableState string               `json:"mergeable_state,omitempty"`
	BaseRef        string               `json:"base_ref"`
	HeadRef        string               `json:"head_ref"`
	HeadSHA        string               `json:"head_sha"`
	URL            string               `json:"url"`
	CI             supervisor.CISummary `json:"ci"`
}

type CandidateState struct {
	Current      *Candidate  `json:"current,omitempty"`
	Alternatives []Candidate `json:"alternatives"`
	Ambiguous    bool        `json:"ambiguous"`
}

type ResultEvidence struct {
	ProjectID   int64                      `json:"project_id"`
	IssueNumber int                        `json:"issue_number"`
	CommentID   int64                      `json:"comment_id"`
	Role        string                     `json:"role"`
	Status      string                     `json:"status"`
	Head        string                     `json:"head,omitempty"`
	Verdict     string                     `json:"verdict,omitempty"`
	Decision    string                     `json:"decision,omitempty"`
	ResumeRole  string                     `json:"resume_role,omitempty"`
	Resolves    json.RawMessage            `json:"resolves,omitempty"`
	EscalateTo  string                     `json:"escalate_to,omitempty"`
	Level       ParseLevel                 `json:"level"`
	Stale       bool                       `json:"stale"`
	Effective   bool                       `json:"effective"`
	CreatedAt   time.Time                  `json:"created_at"`
	Warnings    []Warning                  `json:"warnings"`
	Extensions  map[string]json.RawMessage `json:"extensions,omitempty"`
}

type LatestResults struct {
	Lead        *ResultEvidence `json:"lead,omitempty"`
	Implementor *ResultEvidence `json:"implementor,omitempty"`
	QA          *ResultEvidence `json:"qa,omitempty"`
}

type Attention struct {
	Kind        AttentionKind `json:"kind"`
	Code        string        `json:"code"`
	Explanation string        `json:"explanation"`
}

type Route struct {
	Action       string    `json:"action"`
	TargetRole   string    `json:"target_role,omitempty"`
	LaneKey      string    `json:"lane_key,omitempty"`
	ReasonCode   string    `json:"reason_code"`
	Reason       string    `json:"reason"`
	ExpectedHead string    `json:"expected_head,omitempty"`
	Guards       []string  `json:"guards"`
	Warnings     []Warning `json:"warnings"`
}

type WorkUnitState struct {
	Identity               WorkUnitIdentity     `json:"identity"`
	Lifecycle              string               `json:"lifecycle"`
	Candidate              CandidateState       `json:"candidate"`
	CurrentHead            string               `json:"current_head,omitempty"`
	CI                     supervisor.CISummary `json:"ci"`
	ParsedComments         []ParsedComment      `json:"parsed_comments"`
	LatestResults          LatestResults        `json:"latest_results"`
	ActiveBlocker          *ResultEvidence      `json:"active_blocker,omitempty"`
	QAReviewedHead         string               `json:"qa_reviewed_head,omitempty"`
	QAApprovedHead         string               `json:"qa_approved_head,omitempty"`
	Warnings               []Warning            `json:"warnings"`
	LastMeaningfulActivity time.Time            `json:"last_meaningful_activity"`
	Attention              Attention            `json:"attention"`
	Route                  Route                `json:"route"`
}

type AttentionItem struct {
	Project   ProjectIdentity  `json:"project"`
	WorkUnit  WorkUnitIdentity `json:"work_unit"`
	Attention Attention        `json:"attention"`
	Route     Route            `json:"route"`
}

type ProjectState struct {
	Project   ProjectIdentity `json:"project"`
	WorkUnits []WorkUnitState `json:"work_units"`
	Attention []AttentionItem `json:"attention"`
}

type WorkspaceState struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Projects    []ProjectState  `json:"projects"`
	Attention   []AttentionItem `json:"attention"`
}
