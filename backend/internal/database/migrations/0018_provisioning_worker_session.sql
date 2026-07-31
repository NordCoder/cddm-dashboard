ALTER TABLE session_provision_requests
ADD COLUMN worker_session_id TEXT NOT NULL DEFAULT ''
CHECK (length(worker_session_id) <= 200);

-- Version 17 could already contain provisioned requests. Preserve their exact
-- managed-session identity whenever a materialized delivery provides one.
UPDATE session_provision_requests
SET worker_session_id = COALESCE((
    SELECT d.worker_session_id
    FROM autonomous_command_materializations m
    JOIN delivery_commands d ON d.id = m.delivery_command_id
    WHERE m.project_id = session_provision_requests.project_id
      AND m.provision_request_id = session_provision_requests.id
      AND d.worker_session_id <> ''
    ORDER BY m.created_at DESC, m.id DESC
    LIMIT 1
), '')
WHERE status = 'provisioned'
  AND worker_session_id = '';

-- A standalone version-17 provisioning record has no durable session source to
-- backfill. It must not remain reusable as a healthy managed session after the
-- upgrade; preserve the evidence and fail closed as an uncertain terminal.
UPDATE session_provision_requests
SET status = 'uncertain',
    completion_reason = CASE
        WHEN completion_reason = '' THEN 'missing_durable_session_identity_after_upgrade'
        ELSE completion_reason || ';missing_durable_session_identity_after_upgrade'
    END
WHERE status = 'provisioned'
  AND worker_session_id = '';

INSERT INTO schema_migrations (version, name)
VALUES (18, 'provisioning_worker_session');
