# Controlled pilot guide

This guide prepares a pilot. It does not execute or modify `NordCoder/misak-website#140`.

## 1. Install and start

```bash
gh auth login --git-protocol ssh
cp .env.example .env
# set BROWSER_DELIVERY_ENABLED=true
bash scripts/cddm-up.sh -d
```

Open `http://localhost:1338`.

## 2. Load the extension

Load `extension/` unpacked, confirm the documented extension ID, configure the backend origin, and keep only the intended ChatGPT target active for binding.

## 3. Add the sample Project

```bash
curl -X POST http://localhost:1337/api/projects \
  -H 'Content-Type: application/json' \
  --data @examples/misak-pilot-project.json
```

Record the returned Project ID. This synchronizes repository facts but does not create a command or change MISAK.

## 4. Select the execution profile

Use `cddm-dashboard-resources/v1.0`, `cddm-minimal/v2.0`, `cddm-worker-result/v1`, delivery mode `reviewed`, QA mode `manual_fresh_binding`, and `auto_merge=false`.

## 5. Bind role conversations

For Work Unit #140:

1. activate the intended Lead ChatGPT conversation and bind the Lead lane;
2. activate the intended Implementor conversation and bind the Implementor lane;
3. leave QA unbound until the Dashboard shows **Fresh QA chat required**;
4. create or open a new QA conversation, activate it, and bind QA;
5. after an accepted terminal QA result, Dashboard retires that exact QA binding.

Automatic creation of a new ChatGPT conversation is not required for pilot readiness.

## 6. Run Pilot Readiness

Bash:

```bash
bash scripts/cddm-pilot-readiness.sh http://localhost:1337 <project-id> 140
```

PowerShell:

```powershell
./scripts/cddm-pilot-readiness.ps1 -ApiOrigin http://localhost:1337 -ProjectId <project-id> -IssueNumber 140
```

The command is read-only. It exits successfully only when the endpoint returns:

```text
PILOT READY
status = pilot_ready
```

Expected checks cover GitHub synchronization, resource package/digest, planner, browser worker, Lead lane, Implementor lane, QA mode, CI observability, marker parser, protocol warnings, and `auto_merge=false`.

## 7. Stop boundary

After readiness succeeds, stop. Do not create or deliver the first MISAK Workflow Command as part of installation verification.

## Known limitations

- a human creates/opens the fresh QA ChatGPT conversation;
- the dashboard does not read ChatGPT responses;
- GitHub polling latency controls how quickly terminal comments appear;
- the supported deployment is local/loopback;
- merge remains explicit and is never automatic by default.

## Exact first actions for a later authorized MISAK pilot

1. synchronize the Project and inspect Work Unit #140;
2. confirm the current route, Candidate, Head, CI, warnings, and active command state;
3. bind the role requested by the route;
4. review the versioned prompt and expected Head;
5. explicitly deliver the first command in reviewed mode;
6. wait for a correlated GitHub Issue comment and verified next route.
