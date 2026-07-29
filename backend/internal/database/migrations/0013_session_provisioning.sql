ALTER TABLE project_execution_profiles
    ADD COLUMN chatgpt_project_url TEXT NOT NULL DEFAULT '';

CREATE TABLE session_provision_requests (
    id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    intent_id TEXT NOT NULL REFERENCES workflow_intents(id) ON DELETE CASCADE,
    lane_lease_id TEXT NOT NULL REFERENCES workflow_lane_leases(id) ON DELETE CASCADE,
    lane_key TEXT NOT NULL,
    issue_number INTEGER NOT NULL CHECK (issue_number > 0),
    role TEXT NOT NULL CHECK (role IN ('lead','implementor','qa')),
    expected_head TEXT NOT NULL DEFAULT '',
    attachment_profile TEXT NOT NULL,
    attachments_json TEXT NOT NULL,
    bootstrap_text TEXT NOT NULL,
    session_policy TEXT NOT NULL CHECK (session_policy IN ('persistent_project_lead','fresh_per_intent')),
    chatgpt_project_url TEXT NOT NULL DEFAULT '',
    expected_binding_version INTEGER NOT NULL DEFAULT 0 CHECK (expected_binding_version >= 0),
    status TEXT NOT NULL CHECK (status IN ('pending','claimed','surface_ready','provisioned','safe_failed','uncertain','superseded')),
    claim_id TEXT NOT NULL DEFAULT '',
    claim_owner TEXT NOT NULL DEFAULT '',
    claim_token TEXT NOT NULL DEFAULT '',
    claim_expires_at TEXT NOT NULL DEFAULT '',
    worker_id TEXT NOT NULL DEFAULT '',
    tab_id INTEGER NOT NULL DEFAULT 0 CHECK (tab_id >= 0),
    target_kind TEXT NOT NULL DEFAULT '',
    target_origin TEXT NOT NULL DEFAULT '',
    target_path TEXT NOT NULL DEFAULT '',
    completion_reason TEXT NOT NULL DEFAULT '',
    attachment_evidence_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT '',
    UNIQUE (project_id, intent_id)
);

CREATE UNIQUE INDEX session_provision_requests_claim_id_idx
    ON session_provision_requests(claim_id)
    WHERE claim_id <> '';

CREATE INDEX session_provision_requests_queue_idx
    ON session_provision_requests(status, created_at, id);

CREATE INDEX session_provision_requests_project_idx
    ON session_provision_requests(project_id, status, created_at, id);

CREATE INDEX session_provision_requests_expiry_idx
    ON session_provision_requests(status, claim_expires_at)
    WHERE status = 'claimed';

INSERT INTO schema_migrations (version, name)
VALUES (13, 'session_provisioning');
