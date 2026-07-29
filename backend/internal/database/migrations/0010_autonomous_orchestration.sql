ALTER TABLE project_execution_profiles
    ADD COLUMN autonomy_mode TEXT NOT NULL DEFAULT 'manual_owner_dispatch'
        CHECK (autonomy_mode IN ('manual_owner_dispatch','continuous_dashboard_orchestration'));

ALTER TABLE project_execution_profiles
    ADD COLUMN autonomy_state TEXT NOT NULL DEFAULT 'disabled'
        CHECK (autonomy_state IN ('disabled','enabled','paused','stopped'));

ALTER TABLE project_execution_profiles
    ADD COLUMN control_issue_number INTEGER NOT NULL DEFAULT 0
        CHECK (control_issue_number >= 0);

ALTER TABLE project_execution_profiles
    ADD COLUMN max_active_work_units INTEGER NOT NULL DEFAULT 3
        CHECK (max_active_work_units BETWEEN 1 AND 64);

ALTER TABLE project_execution_profiles
    ADD COLUMN max_parallel_implementors INTEGER NOT NULL DEFAULT 3
        CHECK (max_parallel_implementors BETWEEN 1 AND 64);

ALTER TABLE project_execution_profiles
    ADD COLUMN max_parallel_qa INTEGER NOT NULL DEFAULT 3
        CHECK (max_parallel_qa BETWEEN 1 AND 64);

CREATE TABLE workflow_waves (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    wave_id TEXT NOT NULL,
    control_issue_number INTEGER NOT NULL CHECK (control_issue_number > 0),
    source_command_id TEXT NOT NULL REFERENCES workflow_commands(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('planned','active','waiting','completed','blocked','superseded')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, wave_id)
);

CREATE INDEX workflow_waves_status_idx
    ON workflow_waves(project_id, status, created_at, wave_id);

CREATE TABLE workflow_wave_issues (
    project_id INTEGER NOT NULL,
    wave_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL CHECK (issue_number > 0),
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (project_id, wave_id, issue_number),
    UNIQUE (project_id, wave_id, position),
    FOREIGN KEY (project_id, wave_id)
        REFERENCES workflow_waves(project_id, wave_id) ON DELETE CASCADE
);

CREATE TABLE workflow_intents (
    id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_result_comment_id INTEGER NOT NULL,
    source_command_id TEXT NOT NULL REFERENCES workflow_commands(id) ON DELETE CASCADE,
    action_id TEXT NOT NULL,
    action_type TEXT NOT NULL CHECK (action_type IN ('dispatch','correct','plan_next_wave','merge_candidate','hold','owner_required')),
    repository TEXT NOT NULL,
    issue_number INTEGER NOT NULL DEFAULT 0 CHECK (issue_number >= 0),
    role TEXT NOT NULL DEFAULT '' CHECK (role IN ('','lead','implementor','qa')),
    pr_number INTEGER NOT NULL DEFAULT 0 CHECK (pr_number >= 0),
    expected_head TEXT NOT NULL DEFAULT '',
    expected_previous_head TEXT NOT NULL DEFAULT '',
    reason_code TEXT NOT NULL DEFAULT '',
    decision_category TEXT NOT NULL DEFAULT '',
    wave_id TEXT NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 100 CHECK (priority >= 0),
    lane_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('pending','blocked','claimed','completed','superseded','rejected','ambiguous')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (project_id, source_command_id, action_id),
    FOREIGN KEY (project_id, source_result_comment_id)
        REFERENCES workflow_results(project_id, github_comment_id) ON DELETE CASCADE
);

CREATE INDEX workflow_intents_status_idx
    ON workflow_intents(project_id, status, priority, created_at, id);

CREATE INDEX workflow_intents_issue_idx
    ON workflow_intents(project_id, issue_number, status, created_at);

CREATE INDEX workflow_intents_wave_idx
    ON workflow_intents(project_id, wave_id, status, priority);

INSERT INTO schema_migrations (version, name)
VALUES (10, 'autonomous_orchestration');