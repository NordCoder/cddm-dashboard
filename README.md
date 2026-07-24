# CDDM Dashboard

Stage 4 adds a backend-owned Prompt Planning and Policy layer over the persistent read-only GitHub Supervisor Core and deterministic Stage 3 workflow state.

```text
backend/   Go HTTP API, SQLite persistence, GitHub synchronization, workflow derivation and prompt planning
web/       React and TypeScript frontend
.github/   GitHub Actions verification
.opencode/ restricted OpenCode prompt-planner agent configuration
```

The authoritative flow is:

```text
persisted GitHub snapshot
→ deterministic Stage 3 state and route
→ bounded canonical PromptContext
→ OpenCode composition or static fallback
→ deterministic Policy Engine
→ append-only audit history and API response
```

Stage 3 remains the routing authority. OpenCode may compose explanation and worker-prompt wording, but it cannot select a different action, role, lane or exact Head. Model output is untrusted until policy approval. The backend never launches a per-request LLM process and does not implement direct OpenAI, Anthropic, Codex or other provider adapters.

## Requirements

- Go 1.23+
- Node.js 20.19+ and npm
- Docker with Docker Compose (optional)
- a read-only GitHub token for private repositories or higher API limits
- optionally, a separately managed long-running OpenCode headless server

## Local development

Copy the environment template:

```bash
cp .env.example .env
```

Start the backend:

```bash
cd backend
APP_DATABASE_PATH=./data/cddm.db GITHUB_TOKEN=... go run ./cmd/server
```

The API listens on `http://localhost:8080` by default.

```bash
curl http://localhost:8080/api/health
```

Start the frontend in another terminal:

```bash
cd web
npm ci
npm run dev
```

Open `http://localhost:5173`. The development server proxies `/api` requests to the backend. Set `API_PROXY_TARGET` to override the default `http://localhost:8080` target.

## Project and synchronization API

A Project is a persistent repository identity plus workflow and polling configuration. Tokens are not accepted in request bodies.

```bash
# Create a Project
curl -X POST http://localhost:8080/api/projects \
  -H 'Content-Type: application/json' \
  -d '{
    "owner": "NordCoder",
    "repository": "cddm-dashboard",
    "workflow_mode": "pull_request",
    "polling_enabled": true,
    "poll_interval_seconds": 300
  }'

# List Projects
curl http://localhost:8080/api/projects

# Read one normalized Project snapshot
curl http://localhost:8080/api/projects/1

# Trigger read-only GitHub synchronization
curl -X POST http://localhost:8080/api/projects/1/sync

# Read the workspace snapshot
curl http://localhost:8080/api/workspace

# Delete a Project and its isolated persisted data
curl -X DELETE http://localhost:8080/api/projects/1
```

Each sync is isolated to one Project and transactionally stores open Issues, labels, comments, linked Pull Requests, exact PR Heads and exact-Head CI summaries. GitHub credentials remain process configuration and are not persisted or returned.

## Derived workflow API

Stage 3 derives state at read time from persisted snapshots. Existing Stage 2 contracts remain unchanged.

```bash
curl http://localhost:8080/api/workspace/state
curl http://localhost:8080/api/projects/1/state
curl http://localhost:8080/api/projects/1/work-units/11/state
curl http://localhost:8080/api/attention
curl http://localhost:8080/api/projects/1/attention
```

Each work unit includes repository and Issue identity, lifecycle, Candidate identity, current exact Head, exact-Head CI, parsed terminal results, active blocker, warnings, attention and deterministic route. Routes contain `action`, `target_role`, `lane_key`, reason, expected Head and guards. Stage 3 does not select a browser profile, tab or chat URL.

## Prompt planning API

Prompt generation is scoped by Project and Issue/work unit. The frontend selects only `opencode` or `fallback`; it never supplies provider credentials.

```bash
# Generate with OpenCode when enabled; automatically use fallback according to policy/configuration
curl -X POST http://localhost:8080/api/projects/1/work-units/11/plans \
  -H 'Content-Type: application/json' \
  -d '{"mode":"opencode"}'

# Explicit deterministic static fallback
curl -X POST http://localhost:8080/api/projects/1/work-units/11/plans \
  -H 'Content-Type: application/json' \
  -d '{"mode":"fallback"}'

# Latest plan, append-only history and one historical plan
curl http://localhost:8080/api/projects/1/work-units/11/plans/latest
curl 'http://localhost:8080/api/projects/1/work-units/11/plans?limit=20'
curl http://localhost:8080/api/projects/1/work-units/11/plans/42

# Current bounded context summary and latest policy decision
curl http://localhost:8080/api/projects/1/work-units/11/planning/context
curl http://localhost:8080/api/projects/1/work-units/11/planning/policy

# Configured OpenCode runtime health
curl http://localhost:8080/api/planner/health
```

Generation statuses are:

- `approved`: OpenCode produced a structured plan accepted by policy;
- `rejected`: two bounded attempts were invalid and fallback is disabled;
- `stale`: context, route or current Head changed;
- `fallback`: deterministic template passed the same policy checks;
- `planner_error`: the runtime failed and fallback is disabled.

Concurrent generation requests for the same Project/work unit, context hash and mode are coalesced while in flight. A later regeneration creates a new audit record. Historical plans remain readable but are reported as stale when the authoritative context changes.

### PromptContext

`PromptContext` v1 is built only from the persisted GitHub snapshot and Stage 3 derived state. It includes repository and Issue identity, lifecycle and attention, Candidate, exact Head and CI, latest worker results, active blocker, route, warnings, expected event and bounded evidence. Ordering and JSON serialization are canonical and produce a stable SHA-256 context hash.

Evidence bounds preserve the latest Lead, Implementor and QA terminal events, the active blocker, and relevant Lead dispatch/decision context before filling remaining capacity with recent comments. Credential-like data is redacted. GitHub and OpenCode credentials are never copied into the context, model request log or API response.

Schemas are versioned at:

- `docs/schemas/prompt-context-v1.schema.json`
- `docs/schemas/prompt-plan-v1.schema.json`

### Policy Engine and repair

Policy deterministically checks version, context hash, Candidate/Head freshness, action, role, lane, route guards, blocker and Owner semantics, required prompt sections and prohibited authority. It rejects malformed JSON, prose outside the structured result, invented Heads, routing changes, unsupported completion claims, missing terminal contracts and authority to merge, write GitHub state, dispatch through a browser, approve scope, accept residual risk or disable required CI.

OpenCode receives at most one repair request containing the exact machine-readable violations. There is no retry loop. A second invalid response uses the static fallback when enabled, otherwise the result remains rejected or a planner error.

### Static fallback

The fallback is a deterministic renderer, not a second LLM engine. It uses the same PromptContext and Policy Engine and includes current objective, authoritative state, required action, constraints, prohibited actions, evidence, stop conditions, Initiative Clause and terminal `worker_result` contract. QA routes also include the verdict contract. Owner-attention and non-dispatch routes do not produce a worker-chat prompt.

Fallback is used when explicitly selected or when OpenCode is disabled, unavailable, times out, exceeds the configured request budget, or remains invalid after one repair.

## OpenCode setup

Run OpenCode as a separately managed long-running headless service. Do not launch it once per request. Configure that service with its provider credentials and mount or copy the versioned restricted agent configuration:

```text
.opencode/agents/prompt-planner.md
```

The agent denies shell, file read/edit/write, repository search, web, task and external-directory capabilities. The backend supplies the complete PromptContext and explicitly disables tools at the message boundary as defense in depth.

Example backend configuration:

```bash
OPENCODE_ENABLED=true
OPENCODE_ENDPOINT=http://localhost:4096
OPENCODE_PROVIDER=<configured-provider-id>
OPENCODE_MODEL=<configured-model-id>
OPENCODE_AGENT=prompt-planner
OPENCODE_USERNAME=opencode
OPENCODE_PASSWORD=<server-basic-auth-password>
OPENCODE_TIMEOUT=45s
```

An optional smoke check against a real server may use `/api/planner/health` and one generation request. Real provider credentials and external model network access are not required by CI.

## Audit persistence

SQLite stores append-only planning generations plus equivalent PromptContext, PromptPlan, ModelInvocation and PolicyDecision audit data. Records include context, plan and prompt hashes, source/runtime/provider/model/agent identifiers, mode, latency, status, sanitized error category, timestamps and usage/cost when available. Authorization headers and credentials are never stored.

All planning rows include the Project identity and foreign-key cascade. Reads always filter by Project and Issue, preserving multi-repository isolation.

## Operational responsibility boundary

- **Stage 2:** read-only GitHub synchronization and normalized persistence.
- **Stage 3:** deterministic workflow state, attention and route authority.
- **Stage 4 OpenCode:** wording and composition from the supplied context only.
- **Stage 4 Policy Engine:** deterministic validation, staleness and fallback decision.
- **Future dashboard/browser stages:** presentation and dispatch, outside this stage.

The application does not read ChatGPT Web responses and does not perform GitHub writes, automatic merge, browser binding, prompt insertion or autonomous execution.

See also:

- [CDDM Minimal](docs/cddm-minimal.md)
- [Supervisor Event Contract v1](docs/supervisor-event-contract-v1.md)

## Docker Compose

```bash
docker compose up --build
```

Open `http://localhost:3000`. SQLite data is persisted in `cddm_data`. Compose passes GitHub and OpenCode settings from `.env` without committing secrets. `host.docker.internal` is mapped for connecting the API container to a host-managed OpenCode server.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | Backend listen address |
| `APP_DATABASE_PATH` | `data/cddm.db` | SQLite database path |
| `APP_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |
| `GITHUB_TOKEN` | empty | Read-only GitHub credential |
| `GITHUB_API_BASE_URL` | `https://api.github.com/` | GitHub REST API base URL |
| `GITHUB_REQUEST_TIMEOUT` | `15s` | Per-request GitHub timeout |
| `GITHUB_SYNC_TIMEOUT` | `2m` | End-to-end repository sync timeout |
| `GITHUB_DEFAULT_POLL_INTERVAL` | `5m` | Default interval assigned to new Projects |
| `GITHUB_POLL_SCAN_INTERVAL` | `15s` | Polling coordinator scan cadence |
| `GITHUB_MAX_PAGES` | `10` | Maximum GitHub pages per list surface |
| `GITHUB_MAX_ITEMS` | `500` | Maximum retained GitHub items per list surface |
| `GITHUB_MAX_SYNC_CONCURRENCY` | `4` | Maximum Projects synchronized concurrently |
| `OPENCODE_ENABLED` | `false` | Enable the sole production LLM path |
| `OPENCODE_ENDPOINT` | `http://localhost:4096` | Long-running OpenCode server URL |
| `OPENCODE_PROVIDER` | empty | OpenCode provider identifier; required when enabled |
| `OPENCODE_MODEL` | empty | OpenCode model identifier; required when enabled |
| `OPENCODE_AGENT` | `prompt-planner` | Restricted agent name |
| `OPENCODE_USERNAME` | `opencode` | Basic-auth username |
| `OPENCODE_PASSWORD` | empty | Basic-auth password; process configuration only |
| `OPENCODE_TIMEOUT` | `45s` | Planning request deadline |
| `OPENCODE_MAX_REQUEST_BYTES` | `262144` | Context request budget before fallback |
| `PROMPT_FALLBACK_ENABLED` | `true` | Allow deterministic fallback after runtime/policy failure |
| `PROMPT_EVIDENCE_LIMIT` | `12` | Maximum retained evidence comments; minimum 8 |
| `PROMPT_EVIDENCE_CHARS` | `4000` | Per-evidence Markdown character bound |
| `API_PORT` | `8080` | Host API port in Docker Compose |
| `WEB_PORT` | `3000` | Host web port in Docker Compose |

## Verification

```bash
cd backend
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...

cd ../web
npm ci
npm run build

cd ..
docker compose config --quiet
```
