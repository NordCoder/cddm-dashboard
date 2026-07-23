package supervisor

import "time"

type Project struct {
	ID                  int64      `json:"id"`
	Owner               string     `json:"owner"`
	Repository          string     `json:"repository"`
	WorkflowMode        string     `json:"workflow_mode"`
	PollingEnabled      bool       `json:"polling_enabled"`
	PollIntervalSeconds int64      `json:"poll_interval_seconds"`
	SyncStatus          string     `json:"sync_status"`
	SyncError           string     `json:"sync_error,omitempty"`
	LastSyncStartedAt   *time.Time `json:"last_sync_started_at,omitempty"`
	LastSyncCompletedAt *time.Time `json:"last_sync_completed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type CreateProjectInput struct {
	Owner               string
	Repository          string
	WorkflowMode        string
	PollingEnabled      bool
	PollIntervalSeconds int64
}

type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
}

type Comment struct {
	GitHubID  int64     `json:"github_id"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CISummary struct {
	HeadSHA    string    `json:"head_sha"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	Source     string    `json:"source"`
	DetailsURL string    `json:"details_url,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type PullRequest struct {
	GitHubID       int64     `json:"github_id"`
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	State          string    `json:"state"`
	Draft          bool      `json:"draft"`
	MergeableState string    `json:"mergeable_state,omitempty"`
	BaseRef        string    `json:"base_ref"`
	HeadRef        string    `json:"head_ref"`
	HeadSHA        string    `json:"head_sha"`
	URL            string    `json:"url"`
	UpdatedAt      time.Time `json:"updated_at"`
	CI             CISummary `json:"ci"`
}

type Issue struct {
	GitHubID     int64         `json:"github_id"`
	Number       int           `json:"number"`
	Title        string        `json:"title"`
	State        string        `json:"state"`
	URL          string        `json:"url"`
	Author       string        `json:"author"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	Labels       []Label       `json:"labels"`
	Comments     []Comment     `json:"comments"`
	PullRequests []PullRequest `json:"pull_requests"`
}

type RepositorySnapshot struct {
	FetchedAt time.Time `json:"fetched_at"`
	Issues    []Issue   `json:"issues"`
}

type ProjectSnapshot struct {
	Project Project `json:"project"`
	Issues  []Issue `json:"issues"`
}

type WorkspaceSnapshot struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Projects    []ProjectSnapshot `json:"projects"`
}
