package orchestration

import (
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

const (
	LeaseActive     = "active"
	LeaseReleased   = "released"
	LeaseCompleted  = "completed"
	LeaseSuperseded = "superseded"
	LeaseExpired    = "expired"
)

type Lease struct {
	ID         string     `json:"lease_id"`
	ProjectID  int64      `json:"project_id"`
	LaneKey    string     `json:"lane_key"`
	IntentID   string     `json:"intent_id"`
	ClaimID    string     `json:"claim_id"`
	LeaseOwner string     `json:"lease_owner"`
	LeaseToken string     `json:"lease_token"`
	Status     string     `json:"status"`
	AcquiredAt time.Time  `json:"acquired_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
}

type ClaimRequest struct {
	ProjectID  int64
	ClaimID    string
	LeaseOwner string
	LeaseTTL   time.Duration
	Snapshot   supervisor.ProjectSnapshot
}

type ClaimDecision struct {
	Claimed bool    `json:"claimed"`
	Reason  string  `json:"reason,omitempty"`
	Intent  *Intent `json:"intent,omitempty"`
	Lease   *Lease  `json:"lease,omitempty"`
}

type LeaseTransition struct {
	ProjectID  int64
	LeaseID    string
	LeaseOwner string
	LeaseToken string
	Target     string
}

type Scheduler struct {
	store *Store
	now   func() time.Time
}

func NewScheduler(store *Store) *Scheduler {
	return &Scheduler{store: store, now: func() time.Time { return time.Now().UTC() }}
}
