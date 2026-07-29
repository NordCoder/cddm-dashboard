CREATE TABLE autopilot_controls (
    project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    last_action TEXT NOT NULL DEFAULT 'none',
    updated_at TEXT NOT NULL
);

CREATE TABLE autopilot_circuit_breakers (
    id TEXT PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('project','lane')),
    lane_key TEXT NOT NULL DEFAULT '',
    code TEXT NOT NULL CHECK (code IN (
        'library_resolution_failure',
        'chatgpt_project_scope_mismatch',
        'ambiguous_worker_result',
        'stale_candidate_head',
        'merge_readback_mismatch',
        'github_synchronization_unhealthy',
        'missing_exact_head_ci',
        'worker_session_conflict',
        'uncertain_browser_send',
        'provisioning_conflict',
        'repeated_bounded_failure'
    )),
    reason TEXT NOT NULL,
    recovery_requirements TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT '',
    expected_head TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('open','acknowledged','resolved')),
    occurrence_count INTEGER NOT NULL DEFAULT 1 CHECK (occurrence_count > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    acknowledged_at TEXT NOT NULL DEFAULT '',
    resolved_at TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX autopilot_circuit_breakers_active_scope_idx
    ON autopilot_circuit_breakers(project_id, scope_kind, lane_key, code)
    WHERE status IN ('open','acknowledged');

CREATE INDEX autopilot_circuit_breakers_project_status_idx
    ON autopilot_circuit_breakers(project_id, status, updated_at, id);

CREATE TRIGGER workflow_lane_leases_autopilot_breaker_guard
BEFORE INSERT ON workflow_lane_leases
WHEN NEW.status = 'active'
 AND EXISTS (
    SELECT 1
    FROM autopilot_circuit_breakers b
    WHERE b.project_id = NEW.project_id
      AND b.status IN ('open','acknowledged')
      AND (b.scope_kind = 'project' OR (b.scope_kind = 'lane' AND b.lane_key = NEW.lane_key))
 )
BEGIN
    SELECT RAISE(IGNORE);
END;

INSERT INTO schema_migrations (version, name)
VALUES (17, 'autopilot_operations');
