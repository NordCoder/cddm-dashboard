# CDDM Dashboard

Stage 3 adds a deterministic workflow layer over the persistent read-only GitHub Supervisor Core. The repository remains a small monorepo:

```text
backend/   Go HTTP API, SQLite persistence, GitHub synchronization and derived workflow state
web/       React and TypeScript frontend
.github/   GitHub Actions verification
```

The backend stores project configuration and normalized GitHub snapshots in SQLite. GitHub credentials remain process configuration: they are never stored in project rows, returned by the API or required from the frontend. Worker events, state, attention and routes are derived from the persisted snapshots at read time, so the raw GitHub snapshot remains authoritative after restart.

## Requirements

- Go 1.23+
- Node.js 20.19+ and npm
- Docker with Docker Compose (optional)
- a read-only GitHub token for private repositories or higher API limits

## Local development

Copy the environment template and set `GITHUB_TOKEN` when required:

```bash
cp .env.example .env
```

Start the backend:

```bash
cd backend
APP_DATABASE_PATH=./data/cddm.db GITHUB_TOKEN=... go run ./cmd/server
```

The API listens on `http://localhost:8080` by default. The health endpoint is:

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

## Project API

A Project is a persistent repository identity plus workflow and polling configuration. Tokens are not accepted in these request bodies.

Create a Project:

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
```

`workflow_mode` defaults to `pull_request`. `polling_enabled` defaults to `true`, and `poll_interval_seconds` defaults to `GITHUB_DEFAULT_POLL_INTERVAL`.

List Projects:

```bash
curl http://localhost:8080/api/projects
```

Read one Project and its normalized snapshot:

```bash
curl http://localhost:8080/api/projects/1
```

Trigger a manual synchronization:

```bash
curl -X POST http://localhost:8080/api/projects/1/sync
```

Read the workspace model for all Projects:

```bash
curl http://localhost:8080/api/workspace
```

Delete a Project and its synchronized data:

```bash
curl -X DELETE http://localhost:8080/api/projects/1
```

## Derived workflow API

Derived endpoints parse terminal worker comments, correlate exact PR Heads, classify attention and return the next safe role lane. Existing Stage 2 endpoints remain unchanged.

```bash
# All Projects and work units with derived state and an aggregated attention queue
curl http://localhost:8080/api/workspace/state

# One Project with deterministic work-unit ordering
curl http://localhost:8080/api/projects/1/state

# One Issue/work unit
curl http://localhost:8080/api/projects/1/work-units/6/state

# Workspace attention queue
curl http://localhost:8080/api/attention

# Project attention queue
curl http://localhost:8080/api/projects/1/attention
```

Each work-unit state includes repository and Issue identity, lifecycle label or `unknown`, Candidate/PR identity, current exact Head, CI summary, parsed comments, latest results by role, active blocker, QA reviewed/approved Head, warnings, last meaningful activity, attention and route.

The route contains `action`, `target_role`, deterministic `lane_key`, reason, expected Head, guards and warnings. The lane key is derived only from Project + Issue + role. Stage 3 does not select a browser profile, tab or chat URL.

## Operational model

Every dispatched Lead, Implementor and QA worker publishes one terminal `worker_result`, including when no repository change is needed. Supported statuses are `completed`, `no_op` and `blocked`.

- Implementor `completed` / `no_op` advances to QA when required, otherwise Lead, after successful exact-Head CI.
- Implementor or QA `blocked` advances to Lead first.
- QA `approved` advances to Lead; `changes_required` to Implementor; `inconclusive` to Lead.
- Lead may resume a validated role only after `resolves` identifies the active blocker, or create Owner attention with `owner_required`.
- the latest stale, malformed or ambiguous terminal evidence produces Lead/manual attention rather than a guessed transition.

Operational envelopes are recognized only on their own non-fenced Markdown line, so documentation examples cannot become live events. Historical malformed evidence remains visible but a later valid result can supersede it. A changed PR Head invalidates old Candidate-bound handoff and QA approval evidence; a fresh current-Head QA verdict clears obsolete invalidation attention. CI affects routing only when its Head matches the current Candidate. Multiple open PRs remain explicitly ambiguous. Unknown optional event fields are preserved without making the parser brittle.

See:

- [CDDM Minimal](docs/cddm-minimal.md)
- [Supervisor Event Contract v1](docs/supervisor-event-contract-v1.md)

## Synchronization model

Each sync is isolated to one Project and runs with a context deadline. It stores:

- open Issues and labels;
- Issue comments with stable GitHub identifiers and timestamps;
- Pull Requests that reference synchronized Issues, including base/head refs, draft/state, exact Head SHA and mergeability state when GitHub provides it;
- the latest check-run or combined commit-status summary for the exact PR Head;
- per-Project sync status, timestamps and actionable error text.

Synchronization uses transactional upserts keyed by Project and GitHub identifiers. Repeating the same sync does not create duplicates; a changed PR Head replaces the stored Head and CI summary. A failed repository is marked `failed` without preventing other Projects from synchronizing.

The polling coordinator scans enabled Projects at `GITHUB_POLL_SCAN_INTERVAL` and honors each Project's `poll_interval_seconds`. All list surfaces are bounded by `GITHUB_MAX_PAGES` and `GITHUB_MAX_ITEMS`.

## Docker Compose

Build and start both services:

```bash
docker compose up --build
```

Open `http://localhost:3000`. SQLite data is persisted in the named `cddm_data` volume. Compose passes GitHub configuration from `.env` into the API container without committing secrets.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | Backend listen address |
| `APP_DATABASE_PATH` | `data/cddm.db` | SQLite database path |
| `APP_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |
| `GITHUB_TOKEN` | empty | Read-only GitHub credential; required for private repositories |
| `GITHUB_API_BASE_URL` | `https://api.github.com/` | GitHub REST API base URL, including GitHub Enterprise API roots |
| `GITHUB_REQUEST_TIMEOUT` | `15s` | Per-request HTTP timeout |
| `GITHUB_SYNC_TIMEOUT` | `2m` | End-to-end timeout for one repository sync |
| `GITHUB_DEFAULT_POLL_INTERVAL` | `5m` | Default interval assigned to new Projects |
| `GITHUB_POLL_SCAN_INTERVAL` | `15s` | Coordinator scan cadence |
| `GITHUB_MAX_PAGES` | `10` | Maximum pages per supported GitHub list surface |
| `GITHUB_MAX_ITEMS` | `500` | Maximum retained items per list surface |
| `GITHUB_MAX_SYNC_CONCURRENCY` | `4` | Maximum Projects synchronized concurrently |
| `API_PORT` | `8080` | Host API port in Docker Compose |
| `WEB_PORT` | `3000` | Host web port in Docker Compose |

The application does not log authorization headers or response bodies. GitHub API errors include only the request path, status code and GitHub's short error message.

## Verification

Backend formatting, tests and race detector:

```bash
cd backend
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...
```

Frontend clean install and production build:

```bash
cd web
npm ci
npm run build
```

Compose configuration:

```bash
docker compose config --quiet
```
