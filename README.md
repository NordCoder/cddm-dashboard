# CDDM Dashboard

CDDM Dashboard is a local-first supervisor and worker-loop control plane for GitHub-driven, AI-assisted software delivery.

It synchronizes Issues, Pull Requests, exact Candidate Heads and CI evidence; derives deterministic routes; creates durable Workflow Commands; delivers versioned role prompts to exact ChatGPT conversations through the bundled Chrome extension; and accepts terminal worker results only from validated GitHub Issue comments.

```text
GitHub facts
→ deterministic Work Unit route
→ versioned Workflow Command
→ exact Browser Delivery Command
→ bound Lead / Implementor / QA chat
→ GitHub Issue result comment
→ cddm-worker-result/v1 validation
→ external GitHub verification
→ next route
```

Browser delivery and worker completion are deliberately separate. The application never reads or stores ChatGPT responses.

## Implemented outcome

- read-only multi-repository GitHub synchronization;
- deterministic lifecycle, Candidate, exact-Head CI, QA freshness, blockers, attention and routing;
- Prompt Context, Prompt Plan, Policy Engine, OpenCode composition and deterministic fallback;
- responsive desktop/tablet/mobile Supervisor workspace;
- browser worker identity and exact lane-to-chat binding;
- reviewed delivery, opt-in auto-send, Manual Copy fallback, at-most-once DOM execution and uncertain-delivery recovery;
- repository-owned `resources/cddm-dashboard-resources/v1.0/` package;
- durable Workflow Commands and `cddm-worker-result/v1` Worker Results;
- marker validation, command correlation, conflict handling and GitHub readback verification;
- typed Work Unit execution surfaces and Project Pilot Readiness diagnostics;
- distinct Lead, Implementor and QA bindings with `manual_fresh_binding` QA mode;
- restart, duplicate synchronization, delivered-without-result and downtime-result recovery fixtures.

## Install and start

Recommended GitHub API credential flow:

```bash
gh auth login --git-protocol ssh
cp .env.example .env
```

Enable browser delivery in `.env`:

```env
GITHUB_AUTH_MODE=auto
BROWSER_DELIVERY_ENABLED=true
```

Start with Docker Compose:

```bash
bash scripts/cddm-up.sh -d
```

PowerShell:

```powershell
./scripts/cddm-up.ps1 -d
```

Open:

- Dashboard: `http://localhost:1338`
- API: `http://localhost:1337`

Both ports bind to loopback by default. This build has no public authentication layer and must not be exposed directly to an untrusted LAN or the public internet.

See:

- [Installation](docs/installation.md)
- [Configuration](docs/configuration.md)
- [Browser delivery](docs/browser-delivery.md)
- [Worker-loop protocol](docs/worker-loop.md)
- [Controlled pilot guide](docs/pilot-guide.md)

## Chrome extension

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Choose **Load unpacked** and select `extension/`.
4. Confirm extension ID `biakfbpkfdpniphmoafgldedkbnjfibp`.
5. In extension Options, select `http://localhost:1338` or `http://localhost:1337` as the backend origin.
6. Activate the intended `https://chatgpt.com/c/...` tab before binding it in the current Work Unit.

The extension validates the exact tab, target URL, backend origin, binding version, presence proof, command identity and prompt hash before DOM execution. It never scans for an alternate conversation and never reads response content.

## Role bindings and QA mode

Each Work Unit has three logical lanes:

```text
<owner>/<repository>#<issue>:lead
<owner>/<repository>#<issue>:implementor
<owner>/<repository>#<issue>:qa
```

Lead and Implementor conversations may remain bound while they are healthy. QA uses `manual_fresh_binding`: when the current route requests QA, bind a newly opened QA conversation. After an accepted terminal QA result, Dashboard retires exactly the binding/version used for that command and leaves any newer replacement untouched.

Automatic creation of a new ChatGPT conversation is not required for pilot readiness.

## Delivery and authority

- **Reviewed** delivery requires explicit confirmation of the current backend-approved command.
- **Auto-send** may deliver an already authorized current command when exact route and binding guards pass.
- Auto-send does not decide product scope, create material authority, bypass expected Head/resource guards or retry an uncertain DOM send.
- `auto_merge=false`; merge remains explicit.
- Historical Prompt Plan screens are evidence and never execute current workflow actions.

## Project setup

Create a Project through the API or the dashboard. A sample for the controlled MISAK pilot is provided without executing it:

```bash
curl -X POST http://localhost:1337/api/projects \
  -H 'Content-Type: application/json' \
  --data @examples/misak-pilot-project.json
```

The default execution profile is:

```json
{
  "resource_version": "cddm-dashboard-resources/v1.0",
  "methodology_version": "cddm-minimal/v2.0",
  "result_protocol": "cddm-worker-result/v1",
  "delivery_mode": "reviewed",
  "qa_session_mode": "manual_fresh_binding",
  "auto_merge": false
}
```

## Pilot Readiness

After Project synchronization and role binding, run the read-only diagnostic:

```bash
bash scripts/cddm-pilot-readiness.sh http://localhost:1337 <project-id> <issue-number>
```

PowerShell:

```powershell
./scripts/cddm-pilot-readiness.ps1 -ApiOrigin http://localhost:1337 -ProjectId <project-id> -IssueNumber <issue-number>
```

It exits successfully only when the backend reports `status = pilot_ready` and all required checks pass. Running the diagnostic does not create or deliver a Workflow Command.

## Development

Backend:

```bash
cd backend
GITHUB_AUTH_MODE=gh_cli APP_ADDR=127.0.0.1:1337 APP_DATABASE_PATH=./data/cddm.db go run ./cmd/server
```

Frontend:

```bash
cd web
npm ci
npm run dev
```

The development frontend runs at `http://localhost:5173` and proxies `/api` to `http://localhost:1337`.

## Explicit boundaries

- GitHub facts remain authority for PR, exact Head, CI, QA freshness, mergeability and merge result.
- A Worker Result marker is a claim until correlated and externally verified.
- `delivered` never means worker completion.
- ChatGPT response scraping, semantic response classification and response persistence are prohibited.
- Public multi-user deployment, automatic fresh-conversation creation and automatic merge are future work requiring separate approval.
