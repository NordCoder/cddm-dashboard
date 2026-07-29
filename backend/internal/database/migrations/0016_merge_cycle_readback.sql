ALTER TABLE github_pull_requests
    ADD COLUMN merged INTEGER NOT NULL DEFAULT 0 CHECK (merged IN (0, 1));

ALTER TABLE github_pull_requests
    ADD COLUMN merge_commit_sha TEXT NOT NULL DEFAULT '';

ALTER TABLE github_pull_requests
    ADD COLUMN merged_at TEXT NOT NULL DEFAULT '';

ALTER TABLE workflow_wave_issues
    ADD COLUMN status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned','active','done','blocked','superseded'));

ALTER TABLE workflow_wave_issues
    ADD COLUMN merge_commit_sha TEXT NOT NULL DEFAULT '';

ALTER TABLE workflow_wave_issues
    ADD COLUMN completed_at TEXT NOT NULL DEFAULT '';

CREATE TABLE merge_cycle_readbacks (
    id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    intent_id TEXT NOT NULL REFERENCES workflow_intents(id) ON DELETE CASCADE,
    workflow_command_id TEXT NOT NULL REFERENCES workflow_commands(id) ON DELETE CASCADE,
    source_result_comment_id INTEGER NOT NULL,
    repository TEXT NOT NULL,
    issue_number INTEGER NOT NULL CHECK (issue_number > 0),
    pr_number INTEGER NOT NULL CHECK (pr_number > 0),
    approved_head TEXT NOT NULL,
    reported_merge_commit TEXT NOT NULL,
    observed_merge_commit TEXT NOT NULL DEFAULT '',
    observed_base_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('pending','verified','blocked','ambiguous')),
    reason_code TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    verified_at TEXT NOT NULL DEFAULT '',
    UNIQUE (project_id, intent_id),
    UNIQUE (workflow_command_id)
);

CREATE INDEX merge_cycle_readbacks_status_idx
    ON merge_cycle_readbacks(project_id, status, created_at, id);

CREATE UNIQUE INDEX workflow_intents_next_wave_idx
    ON workflow_intents(project_id, wave_id, action_type)
    WHERE action_type = 'plan_next_wave' AND wave_id <> '';

INSERT INTO schema_migrations (version, name)
VALUES (16, 'merge_cycle_readback');
