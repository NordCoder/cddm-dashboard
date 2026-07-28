# Change — Persist Workflow Commands and Correlated Worker Results

Milestone: M7 — Worker Loop Integration / Pilot Readiness
Issue: #51
Risk: High
Authorized Base: current `main` after #50

## Outcome

Persist the minimum workflow-command and worker-result identities needed to correlate Dashboard-issued role work with one terminal `cddm-worker-result/v1` GitHub Issue comment, while preserving the existing Work Unit router and browser Delivery Command lifecycle.

## Requirements

- Add backend-owned `workflow_commands` and `workflow_results` persistence with Project/Issue isolation.
- Workflow Command identity includes command ID, role, action, resource profile, context hash, expected Head, status and timestamps.
- Worker Result persistence includes command ID, stable GitHub comment ID, role, result, canonical payload, validation state and acceptance time.
- Parse at most one live `cddm-dashboard:result` marker per comment and preserve human-readable Markdown separately.
- Validate version, JSON object shape, role-specific result fields and full SHA identities.
- Correlate known command ID and expected role.
- Distinguish accepted, malformed, unsupported, unbound, wrong_role, stale and ambiguous outcomes.
- Multiple conflicting otherwise-valid terminal results for one command make that command ambiguous; timestamp alone cannot select a winner.
- Repeated GitHub synchronization is idempotent by comment ID and command/result identity.
- A marker without a known command remains unbound evidence and cannot complete a current command.
- Existing historical `supervisor:event` evidence remains supported outside the Dashboard-issued command path.

## Out of Scope

- rendering versioned role resources;
- automatic creation or delivery of workflow commands;
- next-command routing changes;
- frontend readiness;
- automatic merge or Host invocation.

## HARD HOW

- Workflow Command is separate from Browser Delivery Command because transport completion is not role completion.
- Persistence is compact audit/current-state storage, not full event sourcing.
- Marker fields are claims. Consequential GitHub verification belongs to #52.
- Accepted results are exact-command and exact-role bound.
- Command lifecycle is monotonic; terminal or ambiguous commands are never silently returned to awaiting state.
- Project deletion may cascade command/result records.

## Verification

- migration and restart tests;
- valid, malformed, multiple, unsupported, unbound, wrong-role, stale and conflicting markers;
- duplicate synchronization and changed-comment reconciliation;
- exact-Head CI and independent QA.
