ALTER TABLE session_provision_requests
    ADD COLUMN observed_chatgpt_url TEXT NOT NULL DEFAULT '';

ALTER TABLE session_provision_requests
    ADD COLUMN bound_binding_id TEXT NOT NULL DEFAULT '';

ALTER TABLE session_provision_requests
    ADD COLUMN bound_binding_version INTEGER NOT NULL DEFAULT 0 CHECK (bound_binding_version >= 0);

CREATE INDEX session_provision_requests_binding_idx
    ON session_provision_requests(project_id, bound_binding_id)
    WHERE bound_binding_id <> '';

INSERT INTO schema_migrations (version, name)
VALUES (14, 'provisioning_binding_evidence');
