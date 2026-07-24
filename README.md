# CDDM Dashboard

Stage 5 turns the Stage 1–4 foundation into the first usable Supervisor dashboard. The responsive React/TypeScript UI projects canonical backend state for multiple repositories, attention triage, work-unit/Candidate/CI evidence, Prompt Plan generation and history, and local prompt review before manual use in ChatGPT.

```text
backend/   Go HTTP API, SQLite persistence, GitHub synchronization, workflow derivation and prompt planning
web/       Responsive React/TypeScript Supervisor dashboard
.github/   GitHub Actions verification
.opencode/ Restricted OpenCode prompt-planner agent configuration
```

The authoritative flow remains backend-owned:

```text
persisted GitHub snapshot
→ deterministic Stage 3 state and route
→ bounded canonical PromptContext
→ OpenCode composition or static fallback
→ deterministic Policy Engine
→ append-only audit history
→ Stage 5 web projection / manual Copy prompt
```

The frontend does not reimplement lifecycle, routing, Candidate validity, QA freshness, blocker routing, or policy approval. It renders those decisions from Stage 2–4 APIs.

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

Open `http://localhost:5173`. The lightweight development server serves the compiled SPA, falls back to `index.html` for deep links, and proxies `/api` to `http://localhost:8080` by default. Set `API_PROXY_TARGET` to override the backend target.

## Web dashboard MVP

The Stage 5 UI has stable deep-linkable routes:

| Route | Purpose |
| --- | --- |
| `/` | Workspace across configured Projects and the global Attention Queue |
| `/projects/:projectID` | Project sync status, work units, Candidate/CI and worker-result summary |
| `/projects/:projectID/work-units/:issueNumber` | Authoritative work-unit evidence, route, blocker, warnings and planning action |
| `/projects/:projectID/work-units/:issueNumber/plans` | Latest Prompt Plan and generation history |
| `/projects/:projectID/work-units/:issueNumber/plans/:planID` | Historical Prompt Plan detail |
| `/settings` | Backend and planner health summary without credentials |

Workspace and Project views support explicit manual refresh. Data is revalidated on a moderate interval and each Project/work-unit request remains scoped by its backend identity; the UI does not keep a cross-project cache.

### Core user flow

1. Open **Workspace** and triage repository attention.
2. Open the relevant **Project** and inspect sync status, work units, Candidate PR, exact Head, exact-Head CI and latest Lead/Implementor/QA evidence.
3. Open a **Work Unit** and review lifecycle, attention reasons, route, guards, warnings, blocker and evidence.
4. Generate a Prompt Plan in `opencode` or deterministic `fallback` mode when the backend route permits it.
5. Review status, source, action, target role, lane, exact Head, context hash, risk, confidence, guards, prohibited actions and policy violations.
6. Optionally edit the prompt text locally. The UI marks this as **Edited locally**; the backend PromptPlan, PolicyDecision and audit record remain unchanged. Use **Reset to generated prompt** to discard local edits.
7. For an `approved` or `fallback` plan that does not require Owner action, select **Copy prompt**.
8. Manually paste/send that text in the intended ChatGPT chat.

`stale`, `rejected` and `planner_error` generations are review-only and are not presented as dispatch-ready. `owner_required` remains an Owner decision state rather than a normal worker dispatch.

### Stage 5 delivery boundary

Stage 5 stops at manual clipboard delivery:

```text
review plan → optionally edit local copy → Copy prompt → manually paste/send in the intended ChatGPT chat
```

There is no Chrome Extension, browser/tab/chat binding, automatic prompt insertion, automatic send, or ChatGPT DOM/output reading in this stage. Those browser-delivery concerns begin in Stage 6. The web UI never asks for or stores GitHub/OpenCode provider credentials.

### Project management

The existing Stage 2 Project API remains the only frontend GitHub-management surface:

- create a Project by repository identity;
- trigger read-only synchronization;
- delete a Project after explicit destructive confirmation.

GitHub credentials remain process configuration. Deleting a Project removes its isolated synchronized and planning data according to the backend contract.

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

Each sync is isolated to one Project and transactionally stores open Issues, labels, comments, linked Pull Requests, exact PR Heads and exact-Head CI summaries. GitHub credentials are not persisted or returned.

## Derived workflow API

Stage 3 derives state at read time from persisted snapshots. Existing Stage 2 contracts remain unchanged.

```bash
curl http://localhost:8080/api/workspace/state
curl http://localhost:8080/api/projects/1/state
curl http://localhost:8080/api/projects/1/work-units/11/state
curl http://localhost:8080/api/attention
curl http://localhost:8080/api/projects/1/attention
```

Each work unit includes repository and Issue identity, lifecycle, Candidate identity, current exact Head, exact-Head CI, parsed terminal results, active blocker, warnings, attention and deterministic route. Routes contain `action`, `target_role`, `lane_key`, reason, expected Head and guards.

## Prompt planning API

Prompt generation is scoped by Project and Issue/work unit. The frontend selects only `opencode` or `fallback`; it never supplies model/provider credentials.

```bash
# Generate with OpenCode when enabled
curl -X POST http://localhost:8080/api/projects/1/work-units/11/plans \
  -H 'Content-Type: application/json' \
  -d '{"mode":"opencode"}'

# Explicit deterministic fallback
curl -X POST http://localhost:8080/api/projects/1/work-units/11/plans \
  -H 'Content-Type: application/json' \
  -d '{"mode":"fallback"}'

# Latest, history and one historical generation
curl http://localhost:8080/api/projects/1/work-units/11/plans/latest
curl 'http://localhost:8080/api/projects/1/work-units/11/plans?limit=20'
curl http://localhost:8080/api/projects/1/work-units/11/plans/42

# Current bounded context and latest policy decision
curl http://localhost:8080/api/projects/1/work-units/11/planning/context
curl http://localhost:8080/api/projects/1/work-units/11/planning/policy

# OpenCode runtime health
curl http://localhost:8080/api/planner/health
```

Generation statuses are:

- `approved`: OpenCode produced a structured plan accepted by policy;
- `fallback`: deterministic template passed the same policy checks;
- `stale`: context, route or current Head changed;
- `rejected`: bounded model attempts were invalid and fallback is disabled;
- `planner_error`: runtime failed and fallback is disabled.

Concurrent generation requests for the same Project/work unit, context hash and mode are coalesced while in flight. A later regeneration creates a new audit record. Historical plans remain readable and are reported stale when authoritative context changes.

### PromptContext and Policy Engine

`PromptContext` v1 is built only from persisted GitHub state and Stage 3 derived state. It includes repository/Issue identity, lifecycle and attention, Candidate, exact Head and CI, latest worker results, active blocker, route, warnings, expected event and bounded evidence. Canonical serialization yields a stable SHA-256 context hash.

Schemas are versioned at:

- `docs/schemas/prompt-context-v1.schema.json`
- `docs/schemas/prompt-plan-v1.schema.json`

The Policy Engine deterministically checks context hash, Candidate/Head freshness, action, role, lane, guards, blocker/Owner semantics, required prompt sections and prohibited authority. OpenCode receives at most one bounded repair request. Static fallback uses the same PromptContext and policy checks; it is not a second routing engine.

## OpenCode setup

Run OpenCode as a separately managed long-running headless service and use the restricted agent configuration:

```text
.opencode/agents/prompt-planner.md
```

The agent denies shell, repository/file exploration, web and external-directory tools. The backend supplies the complete PromptContext and disables tools at the request boundary as defense in depth.

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

Real provider credentials and external model network access are not required by CI.

## Audit persistence

SQLite stores append-only planning generations and equivalent PromptContext, PromptPlan, ModelInvocation and PolicyDecision audit data. Records are Project/Issue scoped and include context/plan/prompt hashes, source/runtime/provider/model/agent identifiers, mode, status, sanitized error category, timestamps and usage/cost when available. Credentials and authorization headers are never stored.

## Operational responsibility boundary

- **Stage 2:** read-only GitHub synchronization and normalized persistence.
- **Stage 3:** deterministic lifecycle, attention and route authority.
- **Stage 4 OpenCode:** wording/composition from supplied context only.
- **Stage 4 Policy Engine:** deterministic validation, staleness and fallback decision.
- **Stage 5 dashboard:** responsive presentation, Project controls, plan review/local editing and manual Copy prompt.
- **Stage 6+:** browser delivery and later remote/mobile hardening; not implemented here.

The application does not read ChatGPT Web responses, automate merge, or grant the frontend new routing/policy authority.

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
npm test
npm run build

cd ..
docker compose config --quiet
```

GitHub Actions is the authoritative environment for the clean dependency install, backend regression/race suite, frontend tests/build and Docker Compose validation.
