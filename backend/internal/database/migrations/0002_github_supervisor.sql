CREATE TABLE projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner TEXT NOT NULL,
    repository TEXT NOT NULL,
    workflow_mode TEXT NOT NULL,
    polling_enabled INTEGER NOT NULL DEFAULT 1 CHECK (polling_enabled IN (0, 1)),
    poll_interval_seconds INTEGER NOT NULL CHECK (poll_interval_seconds > 0),
    sync_status TEXT NOT NULL DEFAULT 'never',
    sync_error TEXT NOT NULL DEFAULT '',
    last_sync_started_at TEXT,
    last_sync_completed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (owner, repository)
);

CREATE TABLE github_issues (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    github_id INTEGER NOT NULL,
    issue_number INTEGER NOT NULL,
    title TEXT NOT NULL,
    state TEXT NOT NULL,
    url TEXT NOT NULL,
    author TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, github_id),
    UNIQUE (project_id, issue_number)
);

CREATE TABLE github_issue_labels (
    project_id INTEGER NOT NULL,
    issue_github_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (project_id, issue_github_id, name),
    FOREIGN KEY (project_id, issue_github_id)
        REFERENCES github_issues(project_id, github_id) ON DELETE CASCADE
);

CREATE TABLE github_issue_comments (
    project_id INTEGER NOT NULL,
    github_id INTEGER NOT NULL,
    issue_github_id INTEGER NOT NULL,
    body TEXT NOT NULL,
    author TEXT NOT NULL,
    url TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, github_id),
    FOREIGN KEY (project_id, issue_github_id)
        REFERENCES github_issues(project_id, github_id) ON DELETE CASCADE
);

CREATE TABLE github_pull_requests (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    github_id INTEGER NOT NULL,
    pr_number INTEGER NOT NULL,
    title TEXT NOT NULL,
    state TEXT NOT NULL,
    draft INTEGER NOT NULL CHECK (draft IN (0, 1)),
    mergeable_state TEXT NOT NULL DEFAULT '',
    base_ref TEXT NOT NULL,
    head_ref TEXT NOT NULL,
    head_sha TEXT NOT NULL,
    url TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, github_id),
    UNIQUE (project_id, pr_number)
);

CREATE TABLE github_issue_pull_requests (
    project_id INTEGER NOT NULL,
    issue_github_id INTEGER NOT NULL,
    pull_request_github_id INTEGER NOT NULL,
    PRIMARY KEY (project_id, issue_github_id, pull_request_github_id),
    FOREIGN KEY (project_id, issue_github_id)
        REFERENCES github_issues(project_id, github_id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, pull_request_github_id)
        REFERENCES github_pull_requests(project_id, github_id) ON DELETE CASCADE
);

CREATE TABLE github_ci_summaries (
    project_id INTEGER NOT NULL,
    pull_request_github_id INTEGER NOT NULL,
    head_sha TEXT NOT NULL,
    status TEXT NOT NULL,
    conclusion TEXT NOT NULL,
    source TEXT NOT NULL,
    details_url TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, pull_request_github_id),
    FOREIGN KEY (project_id, pull_request_github_id)
        REFERENCES github_pull_requests(project_id, github_id) ON DELETE CASCADE
);

CREATE INDEX github_issues_project_number_idx
    ON github_issues(project_id, issue_number);
CREATE INDEX github_comments_project_issue_idx
    ON github_issue_comments(project_id, issue_github_id, created_at);
CREATE INDEX github_prs_project_number_idx
    ON github_pull_requests(project_id, pr_number);

INSERT INTO schema_migrations (version, name)
VALUES (2, 'github_supervisor');
