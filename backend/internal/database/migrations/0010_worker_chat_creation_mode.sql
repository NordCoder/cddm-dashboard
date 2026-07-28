ALTER TABLE project_execution_profiles
ADD COLUMN chat_creation_mode TEXT NOT NULL DEFAULT 'manual'
CHECK (chat_creation_mode IN ('manual', 'automatic'));

INSERT INTO schema_migrations (version, name)
VALUES (10, 'worker_chat_creation_mode');
