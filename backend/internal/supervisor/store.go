package supervisor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("project not found")
	ErrConflict = errors.New("project already exists")
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) CreateProject(ctx context.Context, input CreateProjectInput) (Project, error) {
	input.Owner = strings.TrimSpace(input.Owner)
	input.Repository = strings.TrimSpace(input.Repository)
	input.WorkflowMode = strings.TrimSpace(input.WorkflowMode)
	if input.Owner == "" || input.Repository == "" || input.WorkflowMode == "" {
		return Project{}, fmt.Errorf("owner, repository and workflow mode are required")
	}
	if strings.Contains(input.Owner, "/") || strings.Contains(input.Repository, "/") {
		return Project{}, fmt.Errorf("owner and repository must not contain slashes")
	}
	if input.PollIntervalSeconds <= 0 {
		return Project{}, fmt.Errorf("poll interval must be positive")
	}

	now := s.now().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO projects (
			owner, repository, workflow_mode, polling_enabled,
			poll_interval_seconds, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, input.Owner, input.Repository, input.WorkflowMode, boolInt(input.PollingEnabled), input.PollIntervalSeconds, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return Project{}, ErrConflict
		}
		return Project{}, fmt.Errorf("create project: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Project{}, fmt.Errorf("read project id: %w", err)
	}
	return s.GetProject(ctx, id)
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, projectSelect+` ORDER BY owner, repository`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return projects, nil
}

func (s *Store) GetProject(ctx context.Context, id int64) (Project, error) {
	project, err := scanProject(s.db.QueryRowContext(ctx, projectSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted project count: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkSyncStarted(ctx context.Context, id int64) error {
	now := s.now().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		UPDATE projects
		SET sync_status = 'syncing', sync_error = '', last_sync_started_at = ?, updated_at = ?
		WHERE id = ?
	`, now, now, id)
	if err != nil {
		return fmt.Errorf("mark sync started: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) MarkSyncFailed(ctx context.Context, id int64, syncErr error) error {
	now := s.now().Format(time.RFC3339Nano)
	message := "sync failed"
	if syncErr != nil {
		message = syncErr.Error()
	}
	if len(message) > 2000 {
		message = message[:2000]
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE projects
		SET sync_status = 'failed', sync_error = ?, last_sync_completed_at = ?, updated_at = ?
		WHERE id = ?
	`, message, now, now, id)
	if err != nil {
		return fmt.Errorf("mark sync failed: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) ReplaceSnapshot(ctx context.Context, projectID int64, snapshot RepositorySnapshot) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin snapshot transaction: %w", err)
	}
	defer tx.Rollback()

	if err := ensureProjectExists(ctx, tx, projectID); err != nil {
		return err
	}
	for _, table := range []string{"github_issue_labels", "github_issue_comments", "github_issue_pull_requests"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE project_id = ?`, projectID); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}

	issueIDs := make([]int64, 0, len(snapshot.Issues))
	pullRequests := make(map[int64]PullRequest)
	for _, issue := range snapshot.Issues {
		issueIDs = append(issueIDs, issue.GitHubID)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO github_issues (
				project_id, github_id, issue_number, title, state, url, author, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(project_id, github_id) DO UPDATE SET
				issue_number = excluded.issue_number,
				title = excluded.title,
				state = excluded.state,
				url = excluded.url,
				author = excluded.author,
				created_at = excluded.created_at,
				updated_at = excluded.updated_at
		`, projectID, issue.GitHubID, issue.Number, issue.Title, issue.State, issue.URL, issue.Author,
			formatTime(issue.CreatedAt), formatTime(issue.UpdatedAt)); err != nil {
			return fmt.Errorf("upsert issue %d: %w", issue.Number, err)
		}

		for _, label := range issue.Labels {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO github_issue_labels (project_id, issue_github_id, name, color, description)
				VALUES (?, ?, ?, ?, ?)
			`, projectID, issue.GitHubID, label.Name, label.Color, label.Description); err != nil {
				return fmt.Errorf("insert issue %d label %q: %w", issue.Number, label.Name, err)
			}
		}
		for _, comment := range issue.Comments {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO github_issue_comments (
					project_id, github_id, issue_github_id, body, author, url, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, projectID, comment.GitHubID, issue.GitHubID, comment.Body, comment.Author, comment.URL,
				formatTime(comment.CreatedAt), formatTime(comment.UpdatedAt)); err != nil {
				return fmt.Errorf("insert issue %d comment %d: %w", issue.Number, comment.GitHubID, err)
			}
		}
		for _, pullRequest := range issue.PullRequests {
			pullRequests[pullRequest.GitHubID] = pullRequest
		}
	}

	for _, pullRequest := range pullRequests {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO github_pull_requests (
				project_id, github_id, pr_number, title, state, draft, mergeable_state,
				base_ref, head_ref, head_sha, url, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(project_id, github_id) DO UPDATE SET
				pr_number = excluded.pr_number,
				title = excluded.title,
				state = excluded.state,
				draft = excluded.draft,
				mergeable_state = excluded.mergeable_state,
				base_ref = excluded.base_ref,
				head_ref = excluded.head_ref,
				head_sha = excluded.head_sha,
				url = excluded.url,
				updated_at = excluded.updated_at
		`, projectID, pullRequest.GitHubID, pullRequest.Number, pullRequest.Title, pullRequest.State,
			boolInt(pullRequest.Draft), pullRequest.MergeableState, pullRequest.BaseRef, pullRequest.HeadRef,
			pullRequest.HeadSHA, pullRequest.URL, formatTime(pullRequest.UpdatedAt)); err != nil {
			return fmt.Errorf("upsert pull request %d: %w", pullRequest.Number, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO github_ci_summaries (
				project_id, pull_request_github_id, head_sha, status, conclusion, source, details_url, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(project_id, pull_request_github_id) DO UPDATE SET
				head_sha = excluded.head_sha,
				status = excluded.status,
				conclusion = excluded.conclusion,
				source = excluded.source,
				details_url = excluded.details_url,
				updated_at = excluded.updated_at
		`, projectID, pullRequest.GitHubID, pullRequest.CI.HeadSHA, pullRequest.CI.Status,
			pullRequest.CI.Conclusion, pullRequest.CI.Source, pullRequest.CI.DetailsURL,
			formatTime(pullRequest.CI.UpdatedAt)); err != nil {
			return fmt.Errorf("upsert pull request %d CI: %w", pullRequest.Number, err)
		}
	}

	for _, issue := range snapshot.Issues {
		for _, pullRequest := range issue.PullRequests {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO github_issue_pull_requests (
					project_id, issue_github_id, pull_request_github_id
				) VALUES (?, ?, ?)
			`, projectID, issue.GitHubID, pullRequest.GitHubID); err != nil {
				return fmt.Errorf("link issue %d to pull request %d: %w", issue.Number, pullRequest.Number, err)
			}
		}
	}

	if err := deleteMissing(ctx, tx, "github_issues", "github_id", projectID, issueIDs); err != nil {
		return err
	}
	pullRequestIDs := make([]int64, 0, len(pullRequests))
	for id := range pullRequests {
		pullRequestIDs = append(pullRequestIDs, id)
	}
	if err := deleteMissing(ctx, tx, "github_pull_requests", "github_id", projectID, pullRequestIDs); err != nil {
		return err
	}

	completedAt := s.now()
	if !snapshot.FetchedAt.IsZero() {
		completedAt = snapshot.FetchedAt.UTC()
	}
	formatted := completedAt.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
		UPDATE projects
		SET sync_status = 'healthy', sync_error = '', last_sync_completed_at = ?, updated_at = ?
		WHERE id = ?
	`, formatted, formatted, projectID); err != nil {
		return fmt.Errorf("mark snapshot healthy: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit snapshot transaction: %w", err)
	}
	return nil
}

func ensureProjectExists(ctx context.Context, tx *sql.Tx, projectID int64) error {
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, projectID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("check project: %w", err)
	}
	return nil
}

func deleteMissing(ctx context.Context, tx *sql.Tx, table, idColumn string, projectID int64, ids []int64) error {
	if len(ids) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE project_id = ?`, projectID); err != nil {
			return fmt.Errorf("delete stale %s: %w", table, err)
		}
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, projectID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `DELETE FROM ` + table + ` WHERE project_id = ? AND ` + idColumn + ` NOT IN (` + strings.Join(placeholders, ",") + `)`
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete stale %s: %w", table, err)
	}
	return nil
}

func requireAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected row count: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return time.Unix(0, 0).UTC().Format(time.RFC3339Nano)
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored timestamp %q: %w", value, err)
	}
	return parsed, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
