# Change #85 — Continuous Autopilot recovery and multi-Issue soak

## Scope

Verify the complete M9–M12 durable loop under restart, duplicate, stale-identity, ambiguity and bounded parallel-Wave conditions. The Change primarily adds deterministic verification and operator evidence. The QA correction also tightens the existing frontend status parser and evidence rendering so incomplete authoritative projection data fails closed; it does not add workflow authority.

## Deterministic evidence

- A production-valid file-backed restart fixture creates linked scheduler, provisioning, binding, Workflow Command, delivery and result records spanning pending, claimed, provisioned, `delivery_pending` and `awaiting_result` stages.
- The fixture captures exact Project, Wave, Issue/lane, Intent, lease, provisioning, binding/session, materialization, Workflow Command, delivery and result identities before close, after reopen and after real Autopilot reconciliation.
- Duplicate Lead action batches, scheduler and provisioning claims, Workflow Command creation and correlated result comments remain idempotent and do not manufacture replacement identities.
- Three-Issue scheduler soak proves Project WIP and Implementor capacity without starving an eligible role lane.
- A separate three-Issue scenario with two simultaneously eligible QA Intents proves `max_parallel_qa` independently of Project WIP and releases the exact waiting QA lane after capacity becomes available.
- Existing merge-Wave fixtures continue proving serialized Lead merge work and exactly one next-Wave planning Intent after all Issues are merged.
- Pause, Stop and project circuit breakers preserve ambiguous Intent, materialization and worker-result evidence.
- Service-worker recovery matrix covers pre-bootstrap, send-reserved, sent, target-observed and provisioned phases without blind bootstrap replay.
- Assignment claim ledger converts a reserved send to durable uncertain state after restart and refuses retargeting under the same claim identity.
- The frontend operator projection requires every authoritative collection, preserves all non-secret backend identity fields, validates nested Project/Issue/PR/Head/lane/lease/binding/command/merge links and rejects incomplete data instead of substituting empty arrays or zero identities.
- The operator runbook requires one correlated evidence chain before recovery, breaker resolution, delivery decisions or cleanup.

## Safety boundaries

- No direct Dashboard merge.
- No arbitrary prompt injection.
- No ChatGPT response scraping.
- No weakening of exact-Head CI, fresh independent QA or merge read-back.
- No retry or retarget based on an incomplete frontend projection.
- Live smoke is disposable, bounded and must not reuse a production work lane.

## Required publication evidence

- full backend test and race suite;
- frontend parser tests and production build;
- Chrome extension recovery suite;
- Runtime CLI and configuration validation;
- exact Candidate Head read-back;
- fresh independent QA before merge.
