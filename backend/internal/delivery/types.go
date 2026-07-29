package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
)

const (
	StatusPending     = "pending"
	StatusClaimed     = "claimed"
	StatusDelivered   = "delivered"
	StatusFailed      = "failed"
	StatusUncertain   = "uncertain"
	StatusCancelled   = "cancelled"
	StatusExpired     = "expired"
	StatusInvalidated = "invalidated"
)

const (
	AuthorityPlanningRoute    = "planning_route"
	AuthorityAutonomousIntent = "autonomous_intent"
)

type BindingSnapshot struct {
	LaneKey         string
	BindingID       string
	BindingVersion  int64
	WorkerID        string
	WorkerSessionID string
	TargetKind      string
	TargetRef       string
	Ready           bool
	PresenceToken   string
}

type BindingResolver interface {
	Resolve(ctx context.Context, projectID int64, laneKey string) (BindingSnapshot, error)
}

type UnavailableBindingResolver struct{}

func (UnavailableBindingResolver) Resolve(context.Context, int64, string) (BindingSnapshot, error) {
	return BindingSnapshot{}, fmt.Errorf("browser binding resolver is unavailable")
}

type BrowserBindingReader interface {
	Get(context.Context, int64, string) (browserbinding.Binding, error)
}

type BrowserBindingResolver struct{ reader BrowserBindingReader }

func NewBrowserBindingResolver(reader BrowserBindingReader) BindingResolver {
	return BrowserBindingResolver{reader: reader}
}

func (r BrowserBindingResolver) Resolve(ctx context.Context, projectID int64, laneKey string) (BindingSnapshot, error) {
	if r.reader == nil {
		return BindingSnapshot{}, ErrUnavailable
	}
	binding, err := r.reader.Get(ctx, projectID, laneKey)
	if err != nil || binding.Readiness != "ready" {
		return BindingSnapshot{}, ErrUnavailable
	}
	return BindingSnapshot{
		LaneKey: binding.LaneKey, BindingID: binding.BindingID, BindingVersion: binding.BindingVersion,
		WorkerID: binding.WorkerID, WorkerSessionID: binding.WorkerSessionID,
		TargetKind: binding.Target.Kind, TargetRef: binding.Target.Origin + binding.Target.Path,
		Ready: true, PresenceToken: binding.PresenceToken,
	}, nil
}

type Confirmation struct {
	PlanID                int64  `json:"plan_id"`
	IdempotencyKey        string `json:"idempotency_key"`
	ExpectedPlanHash      string `json:"expected_plan_hash"`
	ExpectedContextHash   string `json:"expected_context_hash"`
	ExpectedHead          string `json:"expected_head"`
	ExpectedLaneKey       string `json:"expected_lane_key"`
	ExpectedBindingID     string `json:"expected_binding_id"`
	ExpectedBindingVer    int64  `json:"expected_binding_version"`
	ExpectedPresenceToken string `json:"expected_presence_token"`
}

type Command struct {
	ID              string     `json:"id"`
	ProjectID       int64      `json:"project_id"`
	IssueNumber     int        `json:"issue_number"`
	PlanID          int64      `json:"plan_id"`
	PlanHash        string     `json:"plan_hash"`
	ContextHash     string     `json:"context_hash"`
	PromptHash      string     `json:"prompt_hash"`
	Prompt          string     `json:"-"`
	Action          string     `json:"action"`
	TargetRole      string     `json:"target_role"`
	LaneKey         string     `json:"lane_key"`
	ExpectedHead    string     `json:"expected_head"`
	BindingID       string     `json:"binding_id"`
	BindingVersion  int64      `json:"binding_version"`
	WorkerID        string     `json:"worker_id"`
	WorkerSessionID string     `json:"worker_session_id"`
	PresenceToken   string     `json:"-"`
	TargetKind      string     `json:"target_kind"`
	TargetRef       string     `json:"target_ref"`
	AuthorityKind   string     `json:"authority_kind"`
	AuthorityRef    string     `json:"authority_ref,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	ClaimedAt       *time.Time `json:"claimed_at,omitempty"`
	ClaimDeadlineAt *time.Time `json:"claim_deadline_at,omitempty"`
	ClaimID         string     `json:"claim_id,omitempty"`
	ClaimRequestID  string     `json:"claim_request_id,omitempty"`
	TerminalAt      *time.Time `json:"terminal_at,omitempty"`
	OutcomeReason   string     `json:"outcome_reason,omitempty"`
	OutcomeEvidence string     `json:"outcome_evidence,omitempty"`
}

type ClaimRequest struct {
	WorkerID        string `json:"worker_id"`
	WorkerSessionID string `json:"worker_session_id"`
	ClaimRequestID  string `json:"claim_request_id"`
}

type Execution struct {
	Command Command `json:"command"`
	ClaimID string  `json:"claim_id"`
	Prompt  string  `json:"prompt"`
}

type Completion struct {
	CommandID string `json:"command_id,omitempty"`
	ClaimID   string `json:"claim_id"`
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason,omitempty"`
	Evidence  string `json:"evidence,omitempty"`
}
