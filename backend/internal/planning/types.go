package planning

import (
	"encoding/json"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
	"github.com/NordCoder/cddm-dashboard/backend/internal/workflow"
)

const (
	PromptContextVersion = 1
	PromptPlanVersion    = 1

	ModeOpenCode = "opencode"
	ModeFallback = "fallback"

	SourceOpenCode         = "opencode"
	SourceTemplateFallback = "template_fallback"

	StatusApproved     = "approved"
	StatusRejected     = "rejected"
	StatusStale        = "stale"
	StatusFallback     = "fallback"
	StatusPlannerError = "planner_error"
)

type RepositoryIdentity struct {
	ProjectID    int64  `json:"project_id"`
	Owner        string `json:"owner"`
	Repository   string `json:"repository"`
	WorkflowMode string `json:"workflow_mode"`
}

type IssueIdentity struct {
	GitHubID  int64              `json:"github_id"`
	Number    int                `json:"number"`
	Title     string             `json:"title"`
	URL       string             `json:"url"`
	Lifecycle string             `json:"lifecycle"`
	Attention workflow.Attention `json:"attention"`
}

type ResultSummary struct {
	CommentID  int64           `json:"comment_id"`
	Role       string          `json:"role"`
	Status     string          `json:"status"`
	Head       string          `json:"head,omitempty"`
	Verdict    string          `json:"verdict,omitempty"`
	Decision   string          `json:"decision,omitempty"`
	ResumeRole string          `json:"resume_role,omitempty"`
	Resolves   json.RawMessage `json:"resolves,omitempty"`
	EscalateTo string          `json:"escalate_to,omitempty"`
	Stale      bool            `json:"stale"`
	Effective  bool            `json:"effective"`
	CreatedAt  time.Time       `json:"created_at"`
}

type LatestResultSummary struct {
	Lead        *ResultSummary `json:"lead,omitempty"`
	Implementor *ResultSummary `json:"implementor,omitempty"`
	QA          *ResultSummary `json:"qa,omitempty"`
}

type Evidence struct {
	CommentID int64                   `json:"comment_id"`
	Author    string                  `json:"author"`
	URL       string                  `json:"url"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
	Heading   string                  `json:"heading,omitempty"`
	Markdown  string                  `json:"markdown"`
	Level     workflow.ParseLevel     `json:"level"`
	Event     *workflow.WorkerEvent   `json:"event,omitempty"`
	Warnings  []workflow.Warning      `json:"warnings"`
	HardError *workflow.ProtocolError `json:"hard_error,omitempty"`
}

type PromptContext struct {
	Version       int                     `json:"v"`
	Repository    RepositoryIdentity      `json:"repository"`
	Issue         IssueIdentity           `json:"issue"`
	Candidate     workflow.CandidateState `json:"candidate"`
	CurrentHead   string                  `json:"current_head"`
	CI            supervisor.CISummary    `json:"ci"`
	LatestResults LatestResultSummary     `json:"latest_worker_results"`
	ActiveBlocker *ResultSummary          `json:"active_blocker,omitempty"`
	Route         workflow.Route          `json:"route"`
	ExpectedEvent string                  `json:"expected_event"`
	Warnings      []workflow.Warning      `json:"warnings"`
	Evidence      []Evidence              `json:"evidence"`
	ContextHash   string                  `json:"context_hash"`
}

type SourceMetadata struct {
	Kind        string `json:"kind"`
	Runtime     string `json:"runtime"`
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Mode        string `json:"mode"`
	ContextHash string `json:"context_hash"`
}

type PromptPlan struct {
	Version           int                        `json:"v"`
	Action            string                     `json:"action"`
	TargetRole        string                     `json:"target_role"`
	LaneKey           string                     `json:"lane_key"`
	Summary           string                     `json:"summary"`
	Reason            string                     `json:"reason"`
	Risk              string                     `json:"risk"`
	RequiresOwner     bool                       `json:"requires_owner"`
	ExpectedHead      string                     `json:"expected_head"`
	ExpectedEvent     string                     `json:"expected_event"`
	Guards            []string                   `json:"guards"`
	ProhibitedActions []string                   `json:"prohibited_actions"`
	Prompt            string                     `json:"prompt"`
	Confidence        float64                    `json:"confidence"`
	Source            SourceMetadata             `json:"source"`
	Extensions        map[string]json.RawMessage `json:"extensions,omitempty"`
}

type Violation struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type PolicyDecision struct {
	Status      string      `json:"status"`
	Violations  []Violation `json:"violations"`
	ContextHash string      `json:"context_hash"`
	PlanHash    string      `json:"plan_hash,omitempty"`
	DecidedAt   time.Time   `json:"decided_at"`
}

type PolicyAuditDecision struct {
	Attempt  int            `json:"attempt"`
	Final    bool           `json:"final"`
	Decision PolicyDecision `json:"decision"`
}

type Usage struct {
	InputTokens      int64 `json:"input_tokens,omitempty"`
	OutputTokens     int64 `json:"output_tokens,omitempty"`
	ReasoningTokens  int64 `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	CostMicros       int64 `json:"cost_micros,omitempty"`
}

type PlannerRequest struct {
	ContextJSON []byte      `json:"-"`
	Attempt     int         `json:"attempt"`
	Violations  []Violation `json:"violations,omitempty"`
}

type PlannerResponse struct {
	Output   string
	Provider string
	Model    string
	Usage    Usage
}

type GenerationResult struct {
	Status         string         `json:"status"`
	Context        PromptContext  `json:"context"`
	Plan           *PromptPlan    `json:"plan,omitempty"`
	PolicyDecision PolicyDecision `json:"policy_decision"`
	PlanID         int64          `json:"plan_id,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type ContextSummary struct {
	Version       int                `json:"v"`
	ContextHash   string             `json:"context_hash"`
	Repository    RepositoryIdentity `json:"repository"`
	Issue         IssueIdentity      `json:"issue"`
	CurrentHead   string             `json:"current_head,omitempty"`
	Route         workflow.Route     `json:"route"`
	ExpectedEvent string             `json:"expected_event"`
	EvidenceCount int                `json:"evidence_count"`
	WarningCount  int                `json:"warning_count"`
}

type Health struct {
	Enabled  bool   `json:"enabled"`
	Status   string `json:"status"`
	Runtime  string `json:"runtime"`
	Endpoint string `json:"endpoint,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Error    string `json:"error,omitempty"`
}
