ALTER TABLE delivery_commands
    ADD COLUMN authority_kind TEXT NOT NULL DEFAULT 'planning_route';

ALTER TABLE delivery_commands
    ADD COLUMN authority_ref TEXT NOT NULL DEFAULT '';

CREATE TABLE autonomous_command_materializations (
    id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    intent_id TEXT NOT NULL REFERENCES workflow_intents(id) ON DELETE CASCADE,
    lease_id TEXT NOT NULL,
    provision_request_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','materialized','completed','blocked','superseded','ambiguous')),
    reason_code TEXT NOT NULL DEFAULT '',
    workflow_command_id TEXT NOT NULL DEFAULT '',
    delivery_command_id TEXT NOT NULL DEFAULT '',
    context_hash TEXT NOT NULL DEFAULT '',
    prompt_hash TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT '',
    UNIQUE (project_id, intent_id)
);

CREATE UNIQUE INDEX autonomous_command_materializations_workflow_idx
    ON autonomous_command_materializations(workflow_command_id)
    WHERE workflow_command_id <> '';

CREATE UNIQUE INDEX autonomous_command_materializations_delivery_idx
    ON autonomous_command_materializations(delivery_command_id)
    WHERE delivery_command_id <> '';

CREATE INDEX autonomous_command_materializations_status_idx
    ON autonomous_command_materializations(project_id, status, created_at, id);

CREATE INDEX delivery_commands_authority_idx
    ON delivery_commands(authority_kind, authority_ref, status, created_at);

INSERT INTO schema_migrations (version, name)
VALUES (15, 'autonomous_command_materializations');
