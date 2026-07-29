CREATE TABLE workflow_materializations (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_result_comment_id INTEGER NOT NULL,
    source_command_id TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('materialized','skipped','blocked','ambiguous')),
    reason_code TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, source_result_comment_id)
);

CREATE INDEX workflow_materializations_command_idx
    ON workflow_materializations(project_id, source_command_id, status);

INSERT INTO schema_migrations (version, name)
VALUES (11, 'workflow_materializations');