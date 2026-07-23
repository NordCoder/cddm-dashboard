package supervisor

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) ProjectSnapshot(ctx context.Context, projectID int64) (ProjectSnapshot, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return ProjectSnapshot{}, err
	}

	issues, err := s.readIssues(ctx, projectID)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	return ProjectSnapshot{Project: project, Issues: issues}, nil
}

func (s *Store) WorkspaceSnapshot(ctx context.Context) (WorkspaceSnapshot, error) {
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	workspace := WorkspaceSnapshot{
		GeneratedAt: s.now(),
		Projects:    make([]ProjectSnapshot, 0, len(projects)),
	}
	for _, project := range projects {
		snapshot, err := s.ProjectSnapshot(ctx, project.ID)
		if err != nil {
			return WorkspaceSnapshot{}, err
		}
		workspace.Projects = append(workspace.Projects, snapshot)
	}
	return workspace, nil
}

func (s *Store) readIssues(ctx context.Context, projectID int64) ([]Issue, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT github_id, issue_number, title, state, url, author, created_at, updated_at
		FROM github_issues
		WHERE project_id = ?
		ORDER BY issue_number
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read issues: %w", err)
	}

	issues := make([]Issue, 0)
	for rows.Next() {
		var issue Issue
		var createdAt, updatedAt string
		if err := rows.Scan(&issue.GitHubID, &issue.Number, &issue.Title, &issue.State, &issue.URL, &issue.Author, &createdAt, &updatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan issue: %w", err)
		}
		if issue.CreatedAt, err = parseTime(createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		if issue.UpdatedAt, err = parseTime(updatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read issues: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close issue rows: %w", err)
	}

	for index := range issues {
		issues[index].Labels, err = s.readLabels(ctx, projectID, issues[index].GitHubID)
		if err != nil {
			return nil, err
		}
		issues[index].Comments, err = s.readComments(ctx, projectID, issues[index].GitHubID)
		if err != nil {
			return nil, err
		}
		issues[index].PullRequests, err = s.readPullRequests(ctx, projectID, issues[index].GitHubID)
		if err != nil {
			return nil, err
		}
	}
	return issues, nil
}

func (s *Store) readLabels(ctx context.Context, projectID, issueID int64) ([]Label, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, color, description
		FROM github_issue_labels
		WHERE project_id = ? AND issue_github_id = ?
		ORDER BY name
	`, projectID, issueID)
	if err != nil {
		return nil, fmt.Errorf("read labels: %w", err)
	}
	defer rows.Close()
	labels := make([]Label, 0)
	for rows.Next() {
		var label Label
		if err := rows.Scan(&label.Name, &label.Color, &label.Description); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func (s *Store) readComments(ctx context.Context, projectID, issueID int64) ([]Comment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT github_id, body, author, url, created_at, updated_at
		FROM github_issue_comments
		WHERE project_id = ? AND issue_github_id = ?
		ORDER BY created_at, github_id
	`, projectID, issueID)
	if err != nil {
		return nil, fmt.Errorf("read comments: %w", err)
	}
	defer rows.Close()
	comments := make([]Comment, 0)
	for rows.Next() {
		var comment Comment
		var createdAt, updatedAt string
		if err := rows.Scan(&comment.GitHubID, &comment.Body, &comment.Author, &comment.URL, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		if comment.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if comment.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

func (s *Store) readPullRequests(ctx context.Context, projectID, issueID int64) ([]PullRequest, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pr.github_id, pr.pr_number, pr.title, pr.state, pr.draft, pr.mergeable_state,
			pr.base_ref, pr.head_ref, pr.head_sha, pr.url, pr.updated_at,
			ci.head_sha, ci.status, ci.conclusion, ci.source, ci.details_url, ci.updated_at
		FROM github_pull_requests pr
		JOIN github_issue_pull_requests link
			ON link.project_id = pr.project_id AND link.pull_request_github_id = pr.github_id
		LEFT JOIN github_ci_summaries ci
			ON ci.project_id = pr.project_id AND ci.pull_request_github_id = pr.github_id
		WHERE pr.project_id = ? AND link.issue_github_id = ?
		ORDER BY pr.pr_number
	`, projectID, issueID)
	if err != nil {
		return nil, fmt.Errorf("read pull requests: %w", err)
	}
	defer rows.Close()
	pullRequests := make([]PullRequest, 0)
	for rows.Next() {
		var pullRequest PullRequest
		var draft int
		var updatedAt string
		var ciHead, ciStatus, ciConclusion, ciSource, ciDetails, ciUpdated sql.NullString
		if err := rows.Scan(
			&pullRequest.GitHubID, &pullRequest.Number, &pullRequest.Title, &pullRequest.State,
			&draft, &pullRequest.MergeableState, &pullRequest.BaseRef, &pullRequest.HeadRef,
			&pullRequest.HeadSHA, &pullRequest.URL, &updatedAt,
			&ciHead, &ciStatus, &ciConclusion, &ciSource, &ciDetails, &ciUpdated,
		); err != nil {
			return nil, fmt.Errorf("scan pull request: %w", err)
		}
		pullRequest.Draft = draft != 0
		if pullRequest.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		if ciHead.Valid {
			pullRequest.CI = CISummary{
				HeadSHA: ciHead.String, Status: ciStatus.String, Conclusion: ciConclusion.String,
				Source: ciSource.String, DetailsURL: ciDetails.String,
			}
			if ciUpdated.Valid {
				pullRequest.CI.UpdatedAt, err = parseTime(ciUpdated.String)
				if err != nil {
					return nil, err
				}
			}
		}
		pullRequests = append(pullRequests, pullRequest)
	}
	return pullRequests, rows.Err()
}

const projectSelect = `
	SELECT id, owner, repository, workflow_mode, polling_enabled, poll_interval_seconds,
		sync_status, sync_error, last_sync_started_at, last_sync_completed_at, created_at, updated_at
	FROM projects`

type scanner interface {
	Scan(dest ...any) error
}

func scanProject(row scanner) (Project, error) {
	var project Project
	var pollingEnabled int
	var startedAt, completedAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&project.ID, &project.Owner, &project.Repository, &project.WorkflowMode,
		&pollingEnabled, &project.PollIntervalSeconds, &project.SyncStatus, &project.SyncError,
		&startedAt, &completedAt, &createdAt, &updatedAt,
	); err != nil {
		return Project{}, err
	}
	project.PollingEnabled = pollingEnabled != 0
	var err error
	if project.CreatedAt, err = parseTime(createdAt); err != nil {
		return Project{}, err
	}
	if project.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Project{}, err
	}
	if startedAt.Valid {
		value, err := parseTime(startedAt.String)
		if err != nil {
			return Project{}, err
		}
		project.LastSyncStartedAt = &value
	}
	if completedAt.Valid {
		value, err := parseTime(completedAt.String)
		if err != nil {
			return Project{}, err
		}
		project.LastSyncCompletedAt = &value
	}
	return project, nil
}
