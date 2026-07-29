package orchestration

import "time"

const (
	BreakerScopeProject = "project"
	BreakerScopeLane    = "lane"
)

const (
	BreakerOpen         = "open"
	BreakerAcknowledged = "acknowledged"
	BreakerResolved     = "resolved"
)

const (
	BreakerLibraryResolutionFailure    = "library_resolution_failure"
	BreakerChatGPTProjectScopeMismatch = "chatgpt_project_scope_mismatch"
	BreakerAmbiguousWorkerResult       = "ambiguous_worker_result"
	BreakerStaleCandidateHead          = "stale_candidate_head"
	BreakerMergeReadbackMismatch       = "merge_readback_mismatch"
	BreakerGitHubSynchronization       = "github_synchronization_unhealthy"
	BreakerMissingExactHeadCI          = "missing_exact_head_ci"
	BreakerWorkerSessionConflict       = "worker_session_conflict"
	BreakerUncertainBrowserSend        = "uncertain_browser_send"
	BreakerProvisioningConflict        = "provisioning_conflict"
	BreakerRepeatedBoundedFailure      = "repeated_bounded_failure"
)

type AutopilotControl struct {
	ProjectID  int64     `json:"project_id"`
	Revision   int64     `json:"revision"`
	LastAction string    `json:"last_action"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CircuitBreaker struct {
	ID                   string     `json:"id"`
	ProjectID            int64      `json:"project_id"`
	ScopeKind            string     `json:"scope_kind"`
	LaneKey              string     `json:"lane_key,omitempty"`
	Code                 string     `json:"code"`
	Reason               string     `json:"reason"`
	RecoveryRequirements string     `json:"recovery_requirements"`
	Evidence             string     `json:"evidence,omitempty"`
	ExpectedHead         string     `json:"expected_head,omitempty"`
	Status               string     `json:"status"`
	OccurrenceCount      int        `json:"occurrence_count"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	AcknowledgedAt       *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedAt           *time.Time `json:"resolved_at,omitempty"`
}

type BreakerTripInput struct {
	ProjectID        int64
	ExpectedRevision int64
	ScopeKind        string
	LaneKey          string
	Code             string
	Reason           string
	Evidence         string
	ExpectedHead     string
}

type BreakerTransitionInput struct {
	ProjectID        int64
	BreakerID        string
	ExpectedRevision int64
}

type AutopilotQueueItem struct {
	Intent        Intent `json:"intent"`
	WaitingReason string `json:"waiting_reason,omitempty"`
}

// LeaseProjection intentionally excludes the lease token. Operators need the
// durable identity and state, but the bearer credential is not evidence.
type LeaseProjection struct {
	ID         string     `json:"lease_id"`
	ProjectID  int64      `json:"project_id"`
	LaneKey    string     `json:"lane_key"`
	IntentID   string     `json:"intent_id"`
	ClaimID    string     `json:"claim_id"`
	LeaseOwner string     `json:"lease_owner"`
	Status     string     `json:"status"`
	AcquiredAt time.Time  `json:"acquired_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
}

type ProvisioningProjection struct {
	ID                  string    `json:"id"`
	ProjectID           int64     `json:"project_id"`
	IntentID            string    `json:"intent_id"`
	LeaseID             string    `json:"lease_id"`
	LaneKey             string    `json:"lane_key"`
	IssueNumber         int       `json:"issue_number"`
	Role                string    `json:"role"`
	ExpectedHead        string    `json:"expected_head,omitempty"`
	Status              string    `json:"status"`
	CompletionReason    string    `json:"completion_reason,omitempty"`
	WorkerID            string    `json:"worker_id,omitempty"`
	WorkerSessionID     string    `json:"worker_session_id,omitempty"`
	TabID               int       `json:"tab_id,omitempty"`
	ObservedChatGPTURL  string    `json:"observed_chatgpt_url,omitempty"`
	BoundBindingID      string    `json:"bound_binding_id,omitempty"`
	BoundBindingVersion int64     `json:"bound_binding_version,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type CommandProjection struct {
	ProjectID          int64     `json:"project_id"`
	MaterializationID  string    `json:"materialization_id"`
	IntentID           string    `json:"intent_id"`
	LeaseID            string    `json:"lease_id"`
	ProvisionRequestID string    `json:"provision_request_id"`
	LaneKey            string    `json:"lane_key"`
	IssueNumber        int       `json:"issue_number"`
	Role               string    `json:"role"`
	ExpectedHead       string    `json:"expected_head,omitempty"`
	Status             string    `json:"status"`
	ReasonCode         string    `json:"reason_code,omitempty"`
	WorkflowCommandID  string    `json:"workflow_command_id,omitempty"`
	WorkflowStatus     string    `json:"workflow_status,omitempty"`
	DeliveryCommandID  string    `json:"delivery_command_id,omitempty"`
	DeliveryStatus     string    `json:"delivery_status,omitempty"`
	WorkerID           string    `json:"worker_id,omitempty"`
	WorkerSessionID    string    `json:"worker_session_id,omitempty"`
	TabID              int       `json:"tab_id,omitempty"`
	BindingID          string    `json:"binding_id,omitempty"`
	BindingVersion     int64     `json:"binding_version,omitempty"`
	ObservedChatGPTURL string    `json:"observed_chatgpt_url,omitempty"`
	ContextHash        string    `json:"context_hash,omitempty"`
	PromptHash         string    `json:"prompt_hash,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ResultProjection struct {
	ProjectID        int64      `json:"project_id"`
	GitHubCommentID  int64      `json:"github_comment_id"`
	IssueNumber      int        `json:"issue_number"`
	CommandID        string     `json:"command_id"`
	Role             string     `json:"role"`
	Result           string     `json:"result"`
	PayloadHash      string     `json:"payload_hash"`
	ValidationStatus string     `json:"validation_status"`
	ValidationReason string     `json:"validation_reason,omitempty"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	ObservedAt       time.Time  `json:"observed_at"`
}

type AutopilotWarning struct {
	Code         string `json:"code"`
	IntentID     string `json:"intent_id,omitempty"`
	IssueNumber  int    `json:"issue_number,omitempty"`
	PRNumber     int    `json:"pr_number,omitempty"`
	ExpectedHead string `json:"expected_head,omitempty"`
	ObservedHead string `json:"observed_head,omitempty"`
	Message      string `json:"message"`
}

type MergeCycleProjection struct {
	ID                  string    `json:"id"`
	ProjectID           int64     `json:"project_id"`
	IntentID            string    `json:"intent_id"`
	IssueNumber         int       `json:"issue_number"`
	PRNumber            int       `json:"pr_number"`
	ApprovedHead        string    `json:"approved_head"`
	ObservedMergeCommit string    `json:"observed_merge_commit,omitempty"`
	Status              string    `json:"status"`
	ReasonCode          string    `json:"reason_code,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type AutopilotCounts struct {
	PendingIntents        int `json:"pending_intents"`
	BlockedIntents        int `json:"blocked_intents"`
	ClaimedIntents        int `json:"claimed_intents"`
	ActiveLeases          int `json:"active_leases"`
	PendingProvisioning   int `json:"pending_provisioning"`
	ManagedSessions       int `json:"managed_sessions"`
	ActiveCommands        int `json:"active_commands"`
	ActiveCircuitBreakers int `json:"active_circuit_breakers"`
	AmbiguousRecords      int `json:"ambiguous_records"`
}

type AutopilotStatus struct {
	ProjectID         int64                    `json:"project_id"`
	Repository        string                   `json:"repository"`
	SyncStatus        string                   `json:"sync_status"`
	SyncError         string                   `json:"sync_error,omitempty"`
	Profile           ProjectProfile           `json:"profile"`
	Control           AutopilotControl         `json:"control"`
	ActiveWave        *Wave                    `json:"active_wave,omitempty"`
	Intents           []Intent                 `json:"intents"`
	Queue             []AutopilotQueueItem     `json:"queue"`
	Leases            []LeaseProjection        `json:"leases"`
	ActiveLeases      []LeaseProjection        `json:"active_leases"`
	Provisioning      []ProvisioningProjection `json:"provisioning"`
	Commands          []CommandProjection      `json:"commands"`
	Results           []ResultProjection       `json:"results"`
	CircuitBreakers   []CircuitBreaker         `json:"circuit_breakers"`
	Warnings          []AutopilotWarning       `json:"warnings"`
	MergeCycles       []MergeCycleProjection   `json:"merge_cycles"`
	Counts            AutopilotCounts          `json:"counts"`
	ProjectHoldReason string                   `json:"project_hold_reason,omitempty"`
	LeadBusy          bool                     `json:"lead_busy"`
	NextAction        string                   `json:"next_action"`
	GeneratedAt       time.Time                `json:"generated_at"`
}
