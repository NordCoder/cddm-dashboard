ALTER TABLE session_provision_requests
ADD COLUMN worker_session_id TEXT NOT NULL DEFAULT ''
CHECK (length(worker_session_id) <= 200);

INSERT INTO schema_migrations (version, name)
VALUES (18, 'provisioning_worker_session');
