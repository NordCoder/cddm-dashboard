CREATE TABLE workflow_commands (
    id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_number INTEGER NOT NULL,
    identity_key TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('lead','implementor','qa')),
    action TEXT NOT NULL,
    resource_profile TEXT NOT NULL,
    context_hash TEXT NOT NULL,
    expected_head TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('created','delivery_pending','awaiting_result','completed','blocked','inconclusive','failed','ambiguous','superseded')),
    created_at TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT '',
    UNIQUE (project_id, issue_number, identity_key)
);

CREATE INDEX workflow_commands_work_unit_idx
    ON workflow_commands(project_id, issue_number, created_at);
CREATE INDEX workflow_commands_status_idx
    ON workflow_commands(status, created_at);

CREATE TABLE workflow_results (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    github_comment_id INTEGER NOT NULL,
    issue_number INTEGER NOT NULL,
    asserted_command_id TEXT NOT NULL,
    role TEXT NOT NULL,
    result TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    validation_status TEXT NOT NULL CHECK (validation_status IN ('accepted','malformed','unsupported','unbound','wrong_role','stale','ambiguous')),
    validation_reason TEXT NOT NULL DEFAULT '',
    accepted_at TEXT NOT NULL DEFAULT '',
    observed_at TEXT NOT NULL,
    PRIMARY KEY (project_id, github_comment_id)
);

CREATE INDEX workflow_results_command_idx
    ON workflow_results(project_id, asserted_command_id, validation_status);
CREATE INDEX workflow_results_issue_idx
    ON workflow_results(project_id, issue_number, github_comment_id);

INSERT INTO schema_migrations (version, name)
VALUES (7, 'worker_loop');
