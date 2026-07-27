# CDDM Dashboard

CDDM Dashboard is a local/private control plane for supervising AI-assisted software delivery across multiple GitHub repositories. It synchronizes repository state, derives deterministic workflow state, prepares policy-checked Prompt Plans, presents exact Candidate/CI evidence, and can explicitly deliver one approved prompt to one bound ChatGPT conversation through the bundled Chrome extension.

The browser-delivery path is opt-in and never reads ChatGPT response content. Manual Copy remains available independently.

```text
backend/    Go HTTP API, SQLite persistence, GitHub synchronization, workflow, planning and browser-delivery authority
web/        React/TypeScript Supervisor dashboard and confirmed-delivery UI
extension/  Chrome Manifest V3 prompt-delivery executor
.github/    exact-Head GitHub Actions verification
.opencode/  restricted OpenCode prompt-planner configuration
docs/       contracts, schemas and operator documentation
```

## Authoritative flow

```text
persisted GitHub snapshot
→ deterministic workflow state and route
→ bounded PromptContext
→ OpenCode composition or deterministic fallback
→ Policy Engine
→ immutable Prompt Plan + audit record
→ dashboard review
→ Manual Copy
   or
→ explicit browser binding + explicit confirmation
→ backend delivery command
→ exact tracked ChatGPT tab
→ one prompt send
```

Backend state remains authoritative for lifecycle, routing, Candidate validity, blockers, policy, bindings and delivery commands. The frontend and extension only project or execute backend-authorized state.

## Requirements

- Go 1.23+
- Node.js 20.19+ and npm
- Docker with Docker Compose for the standard local deployment
- a read-only GitHub token for private repositories or higher API limits
- optionally, a separately managed long-running OpenCode server
- Chrome/Chromium when confirmed browser delivery is enabled

## Quick start

Copy the environment template:

```bash
cp .env.example .env
```

Set at least `GITHUB_TOKEN` when supervising private repositories, then run:

```bash
docker compose up --build
```

Open `http://localhost:3000`. The web container proxies `/api` to the backend; the backend is also exposed at `http://localhost:8080` by default. SQLite data is persisted in the `cddm_data` volume.

For non-Docker development:

```bash
cd backend
APP_DATABASE_PATH=./data/cddm.db GITHUB_TOKEN=... go run ./cmd/server
```

In another terminal:

```bash
cd web
npm ci
npm run dev
```

Open `http://localhost:5173`. The development server proxies `/api` to `http://localhost:8080` by default; set `API_PROXY_TARGET` to override it.

## Dashboard

Stable routes:

| Route | Purpose |
| --- | --- |
| `/` | Workspace across configured Projects and the global Attention Queue |
| `/projects/:projectID` | Project sync status, work units, Candidate/CI and result summary |
| `/projects/:projectID/work-units/:issueNumber` | Work-unit evidence, route, blockers and planning action |
| `/projects/:projectID/work-units/:issueNumber/plans` | Latest Prompt Plan, local review and generation history |
| `/projects/:projectID/work-units/:issueNumber/plans/:planID` | Historical Prompt Plan detail |
| `/settings` | Backend and planner health without credentials |

Typical flow:

1. Open **Workspace** and choose a Project/work unit requiring attention.
2. Inspect lifecycle, route, blockers, Candidate Head and exact-Head CI.
3. Generate a Prompt Plan with OpenCode or deterministic fallback when the backend route permits it.
4. Review action, role, lane, expected Head, context hash, guards and policy result.
5. Use **Manual Copy** for ordinary clipboard delivery or for locally edited prompt text.
6. For an unchanged current backend prompt, use **Browser Delivery** to bind a live ChatGPT target, review the exact immutable prompt and explicitly confirm one send.
7. Inspect delivery history and terminal state. `uncertain` outcomes are never automatically resent.

`stale`, `rejected` and `planner_error` plans are review-only. Owner-required states are not normal browser dispatches.

## Confirmed Chrome delivery

Browser delivery is disabled by default. Enable it in `.env`:

```env
BROWSER_DELIVERY_ENABLED=true
BROWSER_BINDING_TTL=30s
BROWSER_DELIVERY_PENDING_TTL=5m
BROWSER_DELIVERY_CLAIM_TTL=1m
```

Then rebuild/restart the application and load `extension/` as an unpacked Chrome extension. Configure the extension Options page with the single backend/app origin it may access.

The extension tracks only a supported ChatGPT conversation that the user explicitly activates. It remembers that exact tab identity for the current browser session, revalidates the same tab ID and URL before execution, and never scans for an alternate conversation. Closing or navigating the tracked tab makes delivery unavailable until a current target is proved again.

The delivery safety model includes:

- backend-owned plan/head/lane/binding/presence validation;
- lane/version CAS for bind/rebind/disable;
- one stable idempotency key per frozen confirmation intent;
- durable claim reservation before DOM insertion/send;
- exact target check before insertion and again before send;
- at-most-once DOM send for a claim;
- restart recovery to `uncertain` rather than replay;
- completion transport retry without DOM resend;
- terminal local diagnostic on conflicting completion acknowledgement;
- no ChatGPT response scraping, classification or persistence.

See [Confirmed browser delivery](docs/browser-delivery.md) for installation and operating steps.

## Project and synchronization API

Create, sync and inspect Projects through the backend API:

```bash
curl -X POST http://localhost:8080/api/projects \
  -H 'Content-Type: application/json' \
  -d '{
    "owner": "NordCoder",
    "repository": "cddm-dashboard",
    "workflow_mode": "pull_request",
    "polling_enabled": true,
    "poll_interval_seconds": 300
  }'

curl http://localhost:8080/api/projects
curl -X POST http://localhost:8080/api/projects/1/sync
curl http://localhost:8080/api/workspace/state
curl http://localhost:8080/api/projects/1/work-units/11/state
```

Synchronization is read-only with respect to GitHub. Each Project persists an isolated normalized snapshot of Issues, comments, linked Pull Requests, exact PR Heads and CI summaries. GitHub credentials are process configuration and are not stored in Project records.

## Prompt planning API

```bash
# OpenCode-backed generation
curl -X POST http://localhost:8080/api/projects/1/work-units/11/plans \
  -H 'Content-Type: application/json' \
  -d '{"mode":"opencode"}'

# Explicit deterministic fallback
curl -X POST http://localhost:8080/api/projects/1/work-units/11/plans \
  -H 'Content-Type: application/json' \
  -d '{"mode":"fallback"}'

curl http://localhost:8080/api/projects/1/work-units/11/plans/latest
curl 'http://localhost:8080/api/projects/1/work-units/11/plans?limit=20'
curl http://localhost:8080/api/projects/1/work-units/11/planning/context
curl http://localhost:8080/api/projects/1/work-units/11/planning/policy
curl http://localhost:8080/api/planner/health
```

Generation statuses are `approved`, `fallback`, `stale`, `rejected` and `planner_error`. Historical generations remain readable; current delivery authority is always recalculated from current backend state.

`PromptContext` and `PromptPlan` schemas are versioned under `docs/schemas/`. The Policy Engine checks context identity, Candidate/Head freshness, action, role, lane, guards, blocker/Owner semantics and prohibited authority. Static fallback passes through the same policy checks.

## Browser API surfaces

Operator-facing binding and delivery endpoints:

```text
GET    /api/browser/workers
GET    /api/projects/:projectID/work-units/:issueNumber/browser-binding
PUT    /api/projects/:projectID/work-units/:issueNumber/browser-binding
DELETE /api/projects/:projectID/work-units/:issueNumber/browser-binding
GET    /api/projects/:projectID/work-units/:issueNumber/deliveries
POST   /api/projects/:projectID/work-units/:issueNumber/deliveries
```

Extension-only execution endpoints:

```text
POST /api/browser/workers
POST /api/browser/workers/:workerID/heartbeat
POST /api/browser/deliveries/claim-next
POST /api/browser/deliveries/:commandID/complete
```

The dashboard never claims or completes commands. The extension never creates routing or policy decisions.

## OpenCode setup

Run OpenCode as a separately managed headless service and use `.opencode/agents/prompt-planner.md`. The restricted planner is given the complete bounded PromptContext and has no repository/shell/web exploration authority.

Example configuration:

```env
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

## Persistence and security boundaries

SQLite persists Project snapshots, workflow/planning audit state, browser lane bindings and delivery commands. Credentials and authorization headers are not persisted in frontend state or planning audit records.

The application does not:

- write to supervised GitHub repositories as part of synchronization;
- grant the frontend independent lifecycle/routing/policy authority;
- automatically merge Pull Requests;
- read or infer completion from ChatGPT response content;
- automatically resend an `uncertain` browser delivery.

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
| `OPENCODE_ENABLED` | `false` | Enable OpenCode prompt composition |
| `OPENCODE_ENDPOINT` | `http://localhost:4096` | Long-running OpenCode server URL |
| `OPENCODE_PROVIDER` | empty | OpenCode provider identifier |
| `OPENCODE_MODEL` | empty | OpenCode model identifier |
| `OPENCODE_AGENT` | `prompt-planner` | Restricted agent name |
| `OPENCODE_USERNAME` | `opencode` | Basic-auth username |
| `OPENCODE_PASSWORD` | empty | Basic-auth password; process configuration only |
| `OPENCODE_TIMEOUT` | `45s` | Planning request deadline |
| `OPENCODE_MAX_REQUEST_BYTES` | `262144` | Context request budget before fallback |
| `PROMPT_FALLBACK_ENABLED` | `true` | Allow deterministic fallback |
| `PROMPT_EVIDENCE_LIMIT` | `12` | Maximum retained evidence comments |
| `PROMPT_EVIDENCE_CHARS` | `4000` | Per-evidence Markdown character bound |
| `BROWSER_BINDING_TTL` | `30s` | Browser worker presence freshness window |
| `BROWSER_DELIVERY_ENABLED` | `false` | Enable confirmed browser delivery |
| `BROWSER_DELIVERY_PENDING_TTL` | `5m` | Pending delivery command lifetime |
| `BROWSER_DELIVERY_CLAIM_TTL` | `1m` | Claimed-command acknowledgement deadline |
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

cd ../extension
npm test
node --check src/service-worker.js

cd ..
docker compose config --quiet
```

GitHub Actions checks out the literal Pull Request Head and is the authoritative clean environment for backend tests/race detection, frontend tests/build, extension tests/module validation and Docker Compose validation.

See also:

- [Confirmed browser delivery](docs/browser-delivery.md)
- [Supervisor Event Contract v1](docs/supervisor-event-contract-v1.md)
- [CDDM Minimal](docs/cddm-minimal.md)
