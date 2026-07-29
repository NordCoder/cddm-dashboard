# Change #84 — Autopilot operations controls and circuit breakers

## Scope

Add the operator-facing control plane for continuous Autopilot without changing the GitHub-authoritative execution boundary.

## Delivered contract

- Durable project-scoped optimistic operator revision.
- Idempotent Enable, Pause, Resume and Stop transitions.
- Enable/Resume prerequisites for the exact continuous v2 profile, Project Control Issue, ChatGPT Project URL, healthy completed GitHub synchronization and no unresolved breaker.
- Stop supersedes only safe pre-send pending Intents, materializations and provisioning requests; claimed, delivered, uncertain or ambiguous work remains durable.
- Durable project and exact-lane circuit breakers with open, acknowledged and resolved states, recovery requirements and bounded evidence.
- SQLite claim guard blocks new lane leases only in the affected breaker scope.
- Project Autopilot status API composes profile, revision, active Wave, Intent queue, leases, provisioning, command identities, merge read-back, exact-Head warnings and next action.
- Responsive web operations page with explicit controls, queue/evidence views and breaker recovery actions.

## Safety boundaries

- No direct Dashboard merge path.
- No arbitrary prompt injection.
- No ChatGPT response scraping.
- Pause and Stop do not erase or replay in-flight evidence.
- Acknowledging a breaker does not unblock work; only resolution removes the guard.

## Verification

- Database migration and schema assertions.
- Optimistic revision and idempotency tests.
- Project/lane breaker isolation tests.
- Stop preservation regression.
- HTTP status/control/conflict tests.
- Strict frontend response parser tests and responsive layout rules.
- Exact-Head CI remains the publication gate.
