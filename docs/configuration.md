# Configuration reference

## Network

| Setting | Default | Meaning |
| --- | --- | --- |
| `APP_ADDR` | `127.0.0.1:1337` | Direct Go API address outside Compose |
| `BIND_HOST` | `127.0.0.1` | Docker host publication address |
| `API_PORT` | `1337` | Direct API host port |
| `WEB_PORT` | `1338` | Dashboard host port |

Default operator endpoints:

- Dashboard: `http://localhost:1338`
- API: `http://localhost:1337`

The server enforces loopback Host authorities. Changing `BIND_HOST` does not create a supported public deployment.

## GitHub

`GITHUB_AUTH_MODE=auto` prefers `GITHUB_TOKEN`, then `gh auth token`, then anonymous access. Other modes are `token`, `gh_cli`, and `anonymous`.

Recommended operator setup:

```bash
gh auth login --git-protocol ssh
bash scripts/cddm-up.sh -d
```

Synchronization is read-only with respect to supervised repositories. GitHub credentials are runtime configuration and are not stored in Projects.

## Planning

OpenCode is optional. With `OPENCODE_ENABLED=false`, the deterministic fallback planner remains available when `PROMPT_FALLBACK_ENABLED=true`.

## Browser delivery

```env
BROWSER_DELIVERY_ENABLED=true
BROWSER_BINDING_TTL=30s
BROWSER_DELIVERY_PENDING_TTL=5m
BROWSER_DELIVERY_CLAIM_TTL=1m
```

Browser delivery is exact-target, at-most-once at the DOM boundary, and never reads ChatGPT responses.

## Project execution profile

Each Project lazily receives:

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

`delivery_mode` may be `reviewed` or `auto`. It controls delivery review only. `auto_merge` must remain `false`.

## Sample Project

Use `examples/misak-pilot-project.json` with `POST /api/projects`. After creation, replace `project_id` in `examples/misak-pilot-execution-profile.json` and send it to `PUT /api/projects/<project-id>/execution-profile`.
