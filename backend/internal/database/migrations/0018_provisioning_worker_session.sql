ALTER TABLE session_provision_requests
ADD COLUMN worker_session_id TEXT NOT NULL DEFAULT ''
CHECK (length(worker_session_id) <= 200);

-- Version 17 could already contain provisioned requests. Preserve their exact
-- managed-session identity only when the linked delivery agrees with the
-- provisioning-owned Project, worker and binding identity.
UPDATE session_provision_requests
SET worker_session_id = COALESCE((
    SELECT d.worker_session_id
    FROM autonomous_command_materializations m
    JOIN delivery_commands d ON d.id = m.delivery_command_id
    WHERE m.project_id = session_provision_requests.project_id
      AND m.provision_request_id = session_provision_requests.id
      AND d.project_id = session_provision_requests.project_id
      AND d.worker_id = session_provision_requests.worker_id
      AND d.binding_id = session_provision_requests.bound_binding_id
      AND d.binding_version = session_provision_requests.bound_binding_version
      AND d.worker_session_id <> ''
    ORDER BY m.created_at DESC, m.id DESC
    LIMIT 1
), '')
WHERE status = 'provisioned'
  AND worker_session_id = '';

-- A standalone or identity-conflicting version-17 provisioning record has no
-- trustworthy durable session source. Preserve its evidence but quarantine it
-- as an uncertain terminal rather than treating it as a reusable session.
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
