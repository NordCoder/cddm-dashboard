CREATE TABLE planning_generations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_number INTEGER NOT NULL,
    context_version INTEGER NOT NULL,
    context_hash TEXT NOT NULL,
    context_json TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('opencode', 'fallback')),
    status TEXT NOT NULL CHECK (status IN ('approved', 'rejected', 'stale', 'fallback', 'planner_error')),
    created_at TEXT NOT NULL
);

CREATE TABLE model_invocations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    generation_id INTEGER NOT NULL REFERENCES planning_generations(id) ON DELETE CASCADE,
    attempt INTEGER NOT NULL CHECK (attempt >= 0 AND attempt <= 1),
    runtime TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    agent TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    latency_ms INTEGER NOT NULL CHECK (latency_ms >= 0),
    status TEXT NOT NULL,
    error_category TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    cost_micros INTEGER NOT NULL DEFAULT 0,
    response_hash TEXT NOT NULL,
    response_text TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    UNIQUE (generation_id, attempt)
);

CREATE TABLE prompt_plans (
    generation_id INTEGER PRIMARY KEY REFERENCES planning_generations(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL,
    plan_hash TEXT NOT NULL,
    prompt_hash TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('opencode', 'template_fallback')),
    runtime TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    agent TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    plan_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE policy_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    generation_id INTEGER NOT NULL REFERENCES planning_generations(id) ON DELETE CASCADE,
    decision_order INTEGER NOT NULL CHECK (decision_order >= 0),
    attempt INTEGER NOT NULL DEFAULT -1 CHECK (attempt >= -1 AND attempt <= 1),
    is_final INTEGER NOT NULL CHECK (is_final IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('approved', 'rejected', 'stale', 'fallback', 'planner_error')),
    context_hash TEXT NOT NULL,
    plan_hash TEXT NOT NULL DEFAULT '',
    violations_json TEXT NOT NULL,
    decided_at TEXT NOT NULL,
    UNIQUE (generation_id, decision_order)
);

CREATE UNIQUE INDEX policy_decisions_one_final_idx
    ON policy_decisions(generation_id) WHERE is_final = 1;

CREATE INDEX planning_generations_work_unit_idx
    ON planning_generations(project_id, issue_number, id DESC);
CREATE INDEX planning_generations_context_idx
    ON planning_generations(project_id, issue_number, context_hash);

INSERT INTO schema_migrations (version, name)
VALUES (3, 'prompt_planning');
