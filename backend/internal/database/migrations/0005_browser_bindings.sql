CREATE TABLE browser_workers (
    worker_id TEXT PRIMARY KEY,
    protocol_version TEXT NOT NULL DEFAULT '',
    capabilities_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE browser_lane_bindings (
    binding_id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    lane_key TEXT NOT NULL,
    worker_id TEXT NOT NULL REFERENCES browser_workers(worker_id),
    target_kind TEXT NOT NULL,
    target_origin TEXT NOT NULL,
    target_path TEXT NOT NULL,
    target_label TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    binding_version INTEGER NOT NULL CHECK (binding_version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (project_id, lane_key)
);

CREATE UNIQUE INDEX browser_enabled_target_idx
    ON browser_lane_bindings(worker_id, target_kind, target_origin, target_path)
    WHERE enabled = 1;

INSERT INTO schema_migrations (version, name)
VALUES (5, 'browser_bindings');
