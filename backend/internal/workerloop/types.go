package workerloop

import (
	"encoding/json"
	"time"
)

const (
	CommandCreated         = "created"
	CommandDeliveryPending = "delivery_pending"
	CommandAwaitingResult  = "awaiting_result"
	CommandCompleted       = "completed"
	CommandBlocked         = "blocked"
	CommandInconclusive    = "inconclusive"
	CommandFailed          = "failed"
	CommandAmbiguous       = "ambiguous"
	CommandSuperseded      = "superseded"
)

const (
	ValidationAccepted    = "accepted"
	ValidationMalformed   = "malformed"
	ValidationUnsupported = "unsupported"
	ValidationUnbound     = "unbound"
	ValidationWrongRole   = "wrong_role"
	ValidationStale       = "stale"
	ValidationAmbiguous   = "ambiguous"
)

type Command struct {
	ID              string     `json:"command_id"`
	ProjectID       int64      `json:"project_id"`
	IssueNumber     int        `json:"issue_number"`
	IdentityKey     string     `json:"-"`
	Role            string     `json:"role"`
	Action          string     `json:"action"`
	ResourceProfile string     `json:"resource_version"`
	ContextHash     string     `json:"context_hash"`
	ExpectedHead    string     `json:"expected_head,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type CreateCommandInput struct {
	ID              string
	ProjectID       int64
	IssueNumber     int
	IdentityKey     string
	Role            string
	Action          string
	ResourceProfile string
	ContextHash     string
	ExpectedHead    string
	Status          string
}

type MarkerPayload struct {
	Version          int    `json:"version"`
	Role             string `json:"role"`
	Result           string `json:"result"`
	CommandID        string `json:"command_id"`
	PR               int    `json:"pr,omitempty"`
	Head             string `json:"head,omitempty"`
	ReviewedHead     string `json:"reviewed_head,omitempty"`
	ApprovedHead     string `json:"approved_head,omitempty"`
	BlockingFindings *int   `json:"blocking_findings,omitempty"`
	BlockerType      string `json:"blocker_type,omitempty"`
	ReasonCode       string `json:"reason_code,omitempty"`
	CycleEscalation  string `json:"cycle_escalation,omitempty"`
	NextRole         string `json:"next_role,omitempty"`
}

type ParsedMarker struct {
	Present bool
	Payload MarkerPayload
	JSON    []byte
	Hash    string
	Status  string
	Reason  string
}

type Result struct {
	ProjectID        int64           `json:"project_id"`
	GitHubCommentID  int64           `json:"github_comment_id"`
	IssueNumber      int             `json:"issue_number"`
	CommandID        string          `json:"command_id"`
	Role             string          `json:"role"`
	Result           string          `json:"result"`
	Payload          json.RawMessage `json:"payload"`
	PayloadHash      string          `json:"payload_hash"`
	ValidationStatus string          `json:"validation_status"`
	ValidationReason string          `json:"validation_reason,omitempty"`
	AcceptedAt       *time.Time      `json:"accepted_at,omitempty"`
	ObservedAt       time.Time       `json:"observed_at"`
}
