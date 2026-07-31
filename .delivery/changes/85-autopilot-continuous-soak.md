# Change #85 — Continuous Autopilot recovery and multi-Issue soak

## Scope

Verify the complete M9–M12 durable loop under restart, duplicate, stale-identity, ambiguity and bounded parallel-Wave conditions. The Change adds deterministic verification and strengthens the existing read-only operator projection so incomplete, orphaned or conflicting authoritative data fails closed. It does not add workflow authority.

## Deterministic evidence

- The file-backed restart fixture creates Intents and leases through the scheduler, provisioning through the production queue/finalizer, bindings through the browser-binding service, and materialization, Workflow Command, delivery and links through the real `AutopilotEngine`, `DeliveryCoordinator` and browser-delivery service.
- The fixture spans pending, claimed, standalone provisioned, `delivery_pending` and exact-Head PR-bound QA `awaiting_result` states without direct SQL creation of delivery or materialization records.
- Finalization persists the exact managed worker session directly on the provisioning request in the same transaction as target, binding/version and terminal status.
- Migration 18 backfills version-17 provisioned requests from their exact linked delivery session when available. A legacy standalone provisioned request without a durable source is preserved but quarantined as `uncertain`; it is never treated as a reusable healthy session.
- Before close, immediately after reopen and after real reconciliation/replay, the fixture captures exact Project, Wave, Issue/PR/Head, Intent, lease, provisioning, worker/session/tab, binding, materialization, Workflow Command, delivery, result and link identities plus row cardinality.
- Replaying the same production confirmations after reopen returns the original delivery, materialization and Workflow Command identities; duplicate action batches, scheduler/provisioning claims and result comments manufacture no replacements.
- Three-Issue scheduler soak proves Project WIP and Implementor capacity without starving an eligible role lane.
- A separate three-Issue scenario with two simultaneously eligible QA Intents proves `max_parallel_qa` independently of Project WIP and releases the exact waiting QA lane after capacity becomes available.
- Existing merge-Wave fixtures continue proving serialized Lead merge work and exactly one next-Wave planning Intent after all Issues are merged.
- Pause, Stop and project circuit breakers preserve ambiguous Intent, materialization and worker-result evidence.
- Service-worker recovery covers pre-bootstrap, send-reserved, sent, target-observed and provisioned phases without blind replay.
- Assignment claim recovery converts a reserved send to durable uncertain state and refuses retargeting under the same claim identity.
- The operator projection exposes all durable Intents and leases, provisioning-owned session identities, command provisioning/worker/session/tab/binding links, correlated result-comment identities and merge read-backs while excluding lease tokens.
- `active_leases` is an exact, complete and duplicate-free filtered mirror of authoritative `leases`; every authoritative active lease appears once, no non-active lease appears, every non-secret field matches, and the published active count agrees.
- The queue is an exact duplicate-free projection of every pending, blocked, claimed or ambiguous Intent.
- The frontend requires every authoritative collection, requires a session identity for every `provisioned` request and rejects orphaned, duplicated or conflicting queue, lease, provisioning, command, result and merge records.
- The operator UI renders one row for every Intent, prefers the authoritative active lease over historical lease records, and keeps lease-only `claimed` and standalone `provisioned` stages visible before command materialization.
- The operator runbook requires one correlated evidence row before recovery, breaker resolution, delivery decisions or cleanup.

## Safety boundaries

- No direct Dashboard merge.
- No automatic approval authority.
- No arbitrary prompt injection.
- No ChatGPT response scraping.
- No weakening of exact-Head CI, fresh independent QA or merge read-back.
- No retry or retarget based on an incomplete, orphaned or conflicting projection.
- Lease tokens remain secret and are absent from operator DTOs and UI.
- Live smoke is disposable, bounded and must not reuse a production work lane.

## Required publication evidence

- full backend test and race suite;
- explicit version-17-to-18 upgrade regression;
- frontend parser/evidence tests and production build;
- Chrome extension recovery suite;
- Runtime CLI and configuration validation;
- exact Candidate Head read-back;
- fresh independent QA before merge.

Final publication evidence is recorded in Issue #85 and PR #102, not embedded as mutable workflow identifiers in this contract.
