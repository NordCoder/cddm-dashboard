CREATE TABLE delivery_commands (
    id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_number INTEGER NOT NULL,
    plan_id INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL,
    confirmation_fingerprint TEXT NOT NULL,
    plan_hash TEXT NOT NULL,
    context_hash TEXT NOT NULL,
    prompt_hash TEXT NOT NULL,
    prompt_text TEXT NOT NULL,
    action TEXT NOT NULL,
    target_role TEXT NOT NULL,
    lane_key TEXT NOT NULL,
    expected_head TEXT NOT NULL,
    binding_id TEXT NOT NULL,
    binding_version INTEGER NOT NULL,
    worker_id TEXT NOT NULL,
    worker_session_id TEXT NOT NULL,
    presence_token TEXT NOT NULL,
    target_kind TEXT NOT NULL,
    target_ref TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','claimed','delivered','failed','uncertain','cancelled','expired','invalidated')),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    claimed_at TEXT NOT NULL DEFAULT '',
    claim_deadline_at TEXT NOT NULL DEFAULT '',
    claim_id TEXT NOT NULL DEFAULT '',
    claim_request_id TEXT NOT NULL DEFAULT '',
    terminal_at TEXT NOT NULL DEFAULT '',
    outcome_reason TEXT NOT NULL DEFAULT '',
    outcome_evidence TEXT NOT NULL DEFAULT '',
    UNIQUE (project_id, issue_number, idempotency_key)
);

CREATE INDEX delivery_commands_claim_idx ON delivery_commands(worker_id, status, created_at);
CREATE INDEX delivery_commands_reconcile_idx ON delivery_commands(status, expires_at, claim_deadline_at);
CREATE UNIQUE INDEX delivery_commands_claim_request_idx
    ON delivery_commands(worker_id, claim_request_id)
    WHERE claim_request_id <> '';

INSERT INTO schema_migrations (version, name)
VALUES (6, 'browser_delivery');
