CREATE TABLE project_execution_profiles (
    project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    resource_profile TEXT NOT NULL,
    methodology TEXT NOT NULL,
    result_protocol TEXT NOT NULL,
    delivery_mode TEXT NOT NULL CHECK (delivery_mode IN ('reviewed', 'auto')),
    qa_session_mode TEXT NOT NULL CHECK (qa_session_mode = 'manual_fresh_binding'),
    auto_merge INTEGER NOT NULL DEFAULT 0 CHECK (auto_merge = 0),
    updated_at TEXT NOT NULL
);

INSERT INTO project_execution_profiles (
    project_id, resource_profile, methodology, result_protocol,
    delivery_mode, qa_session_mode, auto_merge, updated_at
)
SELECT id, 'cddm-dashboard-resources/v1.0', 'cddm-minimal/v2.0',
       'cddm-worker-result/v1', 'reviewed', 'manual_fresh_binding', 0, updated_at
FROM projects;

INSERT INTO schema_migrations (version, name)
VALUES (9, 'execution_profiles');
