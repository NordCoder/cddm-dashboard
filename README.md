# CDDM Dashboard

CDDM Dashboard is a local-first supervisor workspace for repository delivery state. It synchronizes GitHub Issues, Pull Requests, exact Candidate Heads and CI evidence, derives backend-owned workflow routing, prepares auditable Prompt Plans, and can deliver an approved prompt through the bundled Chrome extension without reading ChatGPT responses.

## Workspace model

The frontend is structured as an operational workspace rather than a generic card dashboard:

- a persistent repository/health navigation rail;
- attention-first Project and Work Unit views;
- exact-Head, Candidate, CI and worker-result evidence surfaces;
- immutable Prompt Plan review with separate local editing state;
- a Browser Delivery inspector for binding, manual confirmation, automatic delivery and command lifecycle;
- responsive desktop/tablet/mobile layouts with explicit focus, current-page and reduced-motion behavior.

Frontend page controllers, resource runtime, route orchestration, presentation modules and browser-delivery model/view/controller are separated into bounded modules. Production and development servers enforce a strict same-origin CSP and browser security headers.

## GitHub authentication without copying a token

An SSH key authenticates Git transport, but GitHub Issues, Pull Requests and Actions are read through the GitHub REST API. The supported no-copy workflow uses GitHub CLI, which stores the API credential in the system keychain while Git itself can continue using SSH:

```bash
gh auth login --git-protocol ssh
gh auth status
```

`GITHUB_AUTH_MODE=auto` prefers an explicit `GITHUB_TOKEN`, then `gh auth token`, then anonymous access for public repositories. `gh_cli` requires a working GitHub CLI login; `token` requires `GITHUB_TOKEN`; `anonymous` disables authenticated API requests.

For Docker Compose, use the launcher so the credential is resolved on the host and passed only to the running Compose process:

```bash
bash scripts/cddm-up.sh -d
```

PowerShell:

```powershell
./scripts/cddm-up.ps1 -d
```

The launcher does not write the credential to `.env` or the database.

## Run with Docker Compose

Copy the environment template:

```bash
cp .env.example .env
```

Enable browser delivery when the unpacked extension is ready:

```env
BROWSER_DELIVERY_ENABLED=true
```

Then start with GitHub CLI authentication:

```bash
bash scripts/cddm-up.sh -d
```

Or start normally when `GITHUB_TOKEN` is already present in the process environment:

```bash
docker compose up --build -d
```

Open `http://localhost:1338`. The web container proxies `/api` to the backend; the backend is also exposed at `http://localhost:1337`. Both host ports bind to `127.0.0.1` by default because the application has no public authentication layer. SQLite data is persisted in the `cddm_data` volume.

For non-Docker development:

```bash
cd backend
GITHUB_AUTH_MODE=gh_cli APP_ADDR=127.0.0.1:1337 APP_DATABASE_PATH=./data/cddm.db go run ./cmd/server
```

In another terminal:

```bash
cd web
npm ci
npm run dev
```

The development frontend remains on `http://localhost:5173` and proxies `/api` to `http://localhost:1337` by default.

## Browser delivery modes

The extension tracks only a supported ChatGPT conversation that the user explicitly activates. It remembers that exact tab identity for the current browser session, revalidates the same tab ID and URL before execution, and never scans for an alternate conversation. Closing or navigating the tracked tab makes delivery unavailable until a current target is proved again.

Two opt-in delivery modes are available:

- **Review delivery** freezes the current approved backend identities and requires **Confirm and send**.
- **Auto-send** automatically confirms each new exact approved plan when the current browser binding is ready. It is disabled by default and stored separately for each Project/Work Unit.

Auto-send is available only on the current Work Unit and current `/plans` view. It is disabled on historical `/plans/:planID` views so the displayed plan cannot differ from the plan being sent. Manual confirmation and Auto-send are mutually exclusive at both controller and presentation layers.

Auto-send removes the human review screen, not the authority checks. It still uses the backend-approved immutable prompt, exact plan/head/lane/binding/presence CAS identities and one stable idempotency key. A command already created manually for the same exact plan and binding suppresses automatic duplicate creation. Ambiguous transport retries reuse the same intent. The persisted local identity contains only a fingerprint of the current presence proof, never the raw proof.

The delivery safety model includes:

- backend-owned plan/head/lane/binding/presence validation;
- lane/version CAS for bind/rebind/disable;
- one stable idempotency key per exact delivery intent;
- strict command/claim/session identity validation and SHA-256 verification of the immutable prompt;
- serialized durable claim reservation before DOM insertion/send;
- claim authority bound to the exact configured backend origin;
- exact target and backend checks before insertion and again before send;
- identified ChatGPT composer/send selectors without broad generic fallbacks;
- `delivered` only after bounded composer-clear submit acknowledgement; otherwise `uncertain`;
- at-most-once DOM send for a claim;
- restart recovery to `uncertain` rather than replay;
- no ChatGPT response scraping, classification or persistence.

See [Confirmed browser delivery](docs/browser-delivery.md) for installation and operating steps.

## Project and synchronization API

Create, sync and inspect Projects through the backend API:

```bash
curl -X POST http://localhost:1337/api/projects \
  -H 'Content-Type: application/json' \
  -d '{
    "owner": "NordCoder",
    "repository": "cddm-dashboard",
    "workflow_mode": "pull_request",
    "polling_enabled": true,
    "poll_interval_seconds": 300
  }'

curl http://localhost:1337/api/projects
curl -X POST http://localhost:1337/api/projects/1/sync
curl http://localhost:1337/api/workspace/state
curl http://localhost:1337/api/projects/1/work-units/11/state
```

Synchronization is read-only with respect to GitHub. Each Project persists an isolated normalized snapshot of Issues, comments, linked Pull Requests, exact PR Heads and CI summaries. GitHub credentials are process configuration and are not stored in Project records.

## Prompt planning API

```bash
# OpenCode-backed generation
curl -X POST http://localhost:1337/api/projects/1/work-units/11/plans \
  -H 'Content-Type: application/json' \
  -d '{"mode":"opencode"}'

# Explicit deterministic fallback
curl -X POST http://localhost:1337/api/projects/1/work-units/11/plans \
  -H 'Content-Type: application/json' \
  -d '{"mode":"fallback"}'

curl http://localhost:1337/api/projects/1/work-units/11/plans/latest
curl 'http://localhost:1337/api/projects/1/work-units/11/plans?limit=20'
curl http://localhost:1337/api/projects/1/work-units/11/planning/context
```
