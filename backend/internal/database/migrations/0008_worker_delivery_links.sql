CREATE TABLE workflow_delivery_links (
    workflow_command_id TEXT NOT NULL REFERENCES workflow_commands(id) ON DELETE CASCADE,
    delivery_command_id TEXT NOT NULL REFERENCES delivery_commands(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY (workflow_command_id),
    UNIQUE (delivery_command_id)
);

INSERT INTO schema_migrations (version, name)
VALUES (8, 'worker_delivery_links');
