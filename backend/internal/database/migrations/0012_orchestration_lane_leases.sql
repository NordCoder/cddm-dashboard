CREATE TABLE workflow_lane_leases (
    id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    lane_key TEXT NOT NULL,
    intent_id TEXT NOT NULL REFERENCES workflow_intents(id) ON DELETE CASCADE,
    claim_id TEXT NOT NULL,
    lease_owner TEXT NOT NULL,
    lease_token TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active','released','completed','superseded','expired')),
    acquired_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    released_at TEXT NOT NULL DEFAULT '',
    UNIQUE (project_id, claim_id)
);

CREATE UNIQUE INDEX workflow_lane_leases_active_lane_idx
    ON workflow_lane_leases(project_id, lane_key)
    WHERE status = 'active';

CREATE UNIQUE INDEX workflow_lane_leases_active_intent_idx
    ON workflow_lane_leases(project_id, intent_id)
    WHERE status = 'active';

CREATE INDEX workflow_lane_leases_owner_idx
    ON workflow_lane_leases(project_id, lease_owner, status, acquired_at);

CREATE INDEX workflow_lane_leases_expiry_idx
    ON workflow_lane_leases(status, expires_at);

INSERT INTO schema_migrations (version, name)
VALUES (12, 'orchestration_lane_leases');