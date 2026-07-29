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
    source_result_comment_id INTEGER NOT NULL DEFAULT 0,
    repository TEXT NOT NULL,
    issue_number INTEGER NOT NULL CHECK (issue_number > 0),
    pr_number INTEGER NOT NULL CHECK (pr_number > 0),
    approved_head TEXT NOT NULL,
    expected_base_ref TEXT NOT NULL,
    reported_merge_commit TEXT NOT NULL DEFAULT '',
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

-- Autopilot delivery commands are marked before they can be claimed. The
-- existing M12-C1 materialization trigger remains the final authority binder.
CREATE TRIGGER autonomous_delivery_commands_mark_authority
AFTER INSERT ON delivery_commands
WHEN NEW.idempotency_key LIKE 'autopilot:%'
BEGIN
    UPDATE delivery_commands
    SET authority_kind = 'autonomous_intent',
        authority_ref = substr(NEW.idempotency_key, 11)
    WHERE id = NEW.id;
END;

UPDATE delivery_commands
SET authority_kind = 'autonomous_intent',
    authority_ref = substr(idempotency_key, 11)
WHERE idempotency_key LIKE 'autopilot:%';

-- A browser claim is ignored until the exact autonomous materialization is
-- durable. Consequential merge commands additionally require the immutable
-- merge-cycle identity and workflow-command correlation.
CREATE TRIGGER autonomous_delivery_commands_require_activation
BEFORE UPDATE OF status ON delivery_commands
WHEN OLD.status = 'pending'
 AND NEW.status = 'claimed'
 AND OLD.authority_kind = 'autonomous_intent'
 AND (
    NOT EXISTS (
        SELECT 1
        FROM autonomous_command_materializations m
        WHERE m.delivery_command_id = OLD.id
          AND m.intent_id = OLD.authority_ref
          AND m.status = 'materialized'
    )
    OR EXISTS (
        SELECT 1
        FROM workflow_intents i
        WHERE i.id = OLD.authority_ref
          AND i.action_type = 'merge_candidate'
          AND NOT EXISTS (
              SELECT 1
              FROM merge_cycle_readbacks c
              JOIN workflow_delivery_links l
                ON l.workflow_command_id = c.workflow_command_id
              WHERE c.intent_id = i.id
                AND l.delivery_command_id = OLD.id
                AND c.status = 'pending'
          )
    )
 )
BEGIN
    SELECT RAISE(IGNORE);
END;

INSERT INTO schema_migrations (version, name)
VALUES (16, 'merge_cycle_readback');
