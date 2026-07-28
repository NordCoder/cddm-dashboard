ALTER TABLE project_execution_profiles
ADD COLUMN chatgpt_project_url TEXT NOT NULL DEFAULT '';

INSERT INTO schema_migrations (version, name)
VALUES (11, 'chatgpt_project_scope');
