ALTER TABLE github_issues
ADD COLUMN body TEXT NOT NULL DEFAULT '';

INSERT INTO schema_migrations (version, name)
VALUES (4, 'issue_body');
