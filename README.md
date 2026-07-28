# CDDM Dashboard

CDDM Dashboard is a local-first supervisor workspace for repository delivery state. It synchronizes GitHub Issues, Pull Requests, exact Candidate Heads and CI evidence, derives backend-owned workflow routing, prepares auditable Prompt Plans, and can execute an explicitly confirmed prompt through the bundled Chrome extension without reading ChatGPT responses.

## Workspace model

The frontend is structured as an operational workspace rather than a generic card dashboard:

- a persistent repository/health navigation rail;
- attention-first Project and Work Unit views;
- exact-Head, Candidate, CI and worker-result evidence surfaces;
- immutable Prompt Plan review with separate local editing state;
- a right-side Browser Delivery inspector for binding, confirmation and command lifecycle;
- responsive desktop/tablet/mobile layouts with explicit focus, current-page and reduced-motion behavior.

Frontend page controllers, resource runtime, route orchestration, presentation modules and browser-delivery model/view/controller are separated into bounded modules. Production and development servers enforce a strict same-origin CSP and browser security headers.

## Run with Docker Compose

Copy the environment template:

```bash
cp .env.example .env
```

Set at least `GITHUB_TOKEN` when supervising private repositories, then run:

```bash
docker compose up --build
```

Open `http://localhost:3000`. The web container proxies `/api` to the backend; the backend is also exposed at `http://localhost:8080`. Both host ports bind to `127.0.0.1` by default because the application has no public authentication layer. SQLite data is persisted in the `cddm_data` volume.

For non-Docker development:

```bash
cd backend
APP_ADDR=127.0.0.1:8080 APP_DATABASE_PATH=./data/cddm.db GITHUB_TOKEN=... go run ./cmd/server
```

In another terminal:

```bash
cd web
npm ci
npm run dev
```

The extension tracks only a supported ChatGPT conversation that the user explicitly activates. It remembers that exact tab identity for the current browser session, revalidates the same tab ID and URL before execution, and never scans for an alternate conversation. Closing or navigating the tracked tab makes delivery unavailable until a current target is proved again.

The delivery safety model includes:

- backend-owned plan/head/lane/binding/presence validation;
- lane/version CAS for bind/rebind/disable;
- one stable idempotency key per frozen confirmation intent, retained after transport-ambiguous `5xx` or malformed success responses;
- strict command/claim/session identity validation and SHA-256 verification of the claimed immutable prompt;
- serialized durable claim reservation before DOM insertion/send;
- claim authority bound to the exact configured backend origin;
- exact target and backend-configuration checks before insertion and again before send;
- identified ChatGPT composer/send selectors without broad generic contenteditable/submit fallbacks;
- `delivered` only after bounded composer-clear submit acknowledgement; otherwise `uncertain`;
- at-most-once DOM send for a claim;
- restart recovery to `uncertain` rather than replay;
- time-bounded backend requests and completion transport retry without DOM resend;
- terminal local diagnostic on conflicting or definitive rejected completion acknowledgement;
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
```
