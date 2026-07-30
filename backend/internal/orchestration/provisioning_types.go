package orchestration

import (
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
)

const (
	ProvisionPending      = "pending"
	ProvisionClaimed      = "claimed"
	ProvisionSurfaceReady = "surface_ready"
	ProvisionProvisioned  = "provisioned"
	ProvisionSafeFailed   = "safe_failed"
	ProvisionUncertain    = "uncertain"
	ProvisionSuperseded   = "superseded"
)

const (
	SessionPolicyPersistentLead = "persistent_project_lead"
	SessionPolicyFreshPerIntent = "fresh_per_intent"
)

type ProvisionRequest struct {
	ID                     string                    `json:"request_id"`
	ProjectID              int64                     `json:"project_id"`
	IntentID               string                    `json:"intent_id"`
	LaneLeaseID            string                    `json:"lane_lease_id"`
	LaneKey                string                    `json:"lane_key"`
	IssueNumber            int                       `json:"issue_number"`
	Role                   string                    `json:"role"`
	ExpectedHead           string                    `json:"expected_head,omitempty"`
	AttachmentProfile      string                    `json:"attachment_profile"`
	Attachments            []string                  `json:"attachments"`
	BootstrapText          string                    `json:"bootstrap_text"`
	SessionPolicy          string                    `json:"session_policy"`
	ChatGPTProjectURL      string                    `json:"chatgpt_project_url,omitempty"`
	ExpectedBindingVersion int64                     `json:"expected_binding_version"`
	Status                 string                    `json:"status"`
	ClaimID                string                    `json:"claim_id,omitempty"`
	ClaimOwner             string                    `json:"claim_owner,omitempty"`
	ClaimToken             string                    `json:"claim_token,omitempty"`
	ClaimExpiresAt         *time.Time                `json:"claim_expires_at,omitempty"`
	WorkerID               string                    `json:"worker_id,omitempty"`
	WorkerSessionID        string                    `json:"worker_session_id,omitempty"`
	TabID                  int                       `json:"tab_id,omitempty"`
	Target                 *browserbinding.TargetRef `json:"target,omitempty"`
	ObservedChatGPTURL     string                    `json:"observed_chatgpt_url,omitempty"`
	BoundBindingID         string                    `json:"bound_binding_id,omitempty"`
	BoundBindingVersion    int64                     `json:"bound_binding_version,omitempty"`
	CompletionReason       string                    `json:"completion_reason,omitempty"`
	AttachmentEvidence     []string                  `json:"attachment_evidence,omitempty"`
	CreatedAt              time.Time                 `json:"created_at"`
	UpdatedAt              time.Time                 `json:"updated_at"`
	CompletedAt            *time.Time                `json:"completed_at,omitempty"`
}

type EnqueueProvisioningInput struct {
	ProjectID  int64
	LeaseID    string
	LeaseOwner string
	LeaseToken string
}

type ProvisionClaimInput struct {
	ClaimID    string
	ClaimOwner string
	ClaimTTL   time.Duration
}

type ProvisionCompletionInput struct {
	RequestID          string
	ClaimOwner         string
	ClaimToken         string
	Outcome            string
	Reason             string
	WorkerID           string
	TabID              int
	Target             *browserbinding.TargetRef
	AttachmentEvidence []string
}

type FinalizeProvisioningInput struct {
	RequestID          string
	ClaimOwner         string
	ClaimToken         string
	WorkerID           string
	TabID              int
	Target             browserbinding.TargetRef
	ObservedChatGPTURL string
	AttachmentEvidence []string
}
