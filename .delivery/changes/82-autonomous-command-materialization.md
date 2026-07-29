# Change #82 — Autonomous command materialization

## Outcome

Connect one exact M10 scheduler Intent and M11 provisioned role session to the existing immutable Workflow Command, Browser Delivery and GitHub result loop without creating a parallel prompt or transport authority.

## Authorized Base

`c325e8fb95c5bd954dea4bdb418cd09d810bd17d`

## Delivered contract

```text
pending Workflow Intent
→ dashboard-autopilot lane lease
→ durable session provisioning
→ exact provisioned role binding
→ deterministic current-route v2 Prompt Plan
→ immutable Workflow Command
→ existing Browser Delivery Command
→ browser delivered/awaiting_result
→ accepted command-bound GitHub result
→ completed Intent, lease and materialization
```

### Persistence

Migration `0015_autonomous_command_materializations` adds:

- one materialization identity per Project and Intent;
- scheduler and canonical delivery lane identities;
- exact plan, Workflow Command and Delivery Command identities;
- pending, materialized, completed, blocked, superseded and ambiguous outcomes;
- explicit delivery authority kind/reference;
- an atomic authority-binding trigger when a materialization becomes executable.

### Prompt authority

Autonomous prompt text is rendered internally from the current synchronized GitHub snapshot and deterministic Stage 3 route. The Intent and browser cannot supply or replace prompt text. A non-empty expected Head must match the current Candidate; an omitted Implementor Head is derived from the current route and is not a retarget operation.

### Binding authority

Canonical Stage 3 delivery lanes remain unchanged. A composite resolver may map a canonical lane to exactly one active autonomous materialization and its exact M10/M11 scheduler binding. A normal/manual binding takes precedence; ambiguous mappings fail closed.

### Result authority

Browser `delivered` remains transport evidence only. The exact M10 lease and Intent complete only after an accepted v2 GitHub result correlated to the immutable Workflow Command. Lead `merged` evidence is intentionally reserved for Change #83 independent merge read-back.

## Compatibility

- Manual v1 planning and delivery remain the default.
- Existing delivery claim/completion and extension protocols remain unchanged.
- Existing M10 typed Lead action materialization remains unchanged and is composed through a separate result pipeline.
- No direct Dashboard merge, next-Wave completion, response scraping or arbitrary prompt injection is introduced.

## Verification

Focused regressions cover:

- deterministic autonomous plan persistence;
- role and Head drift rejection;
- exact scheduler lease and finalized M11 binding;
- one-per-Intent command materialization and restart idempotency;
- canonical-to-scheduler binding resolution;
- atomic delivery authority identity;
- accepted result completion of materialization, Intent and lease;
- finalized provisioning identity after binding-version advancement;
- existing frontend, extension, package, backend and race suites.
