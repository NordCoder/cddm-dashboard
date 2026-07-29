# Change #85 — Continuous Autopilot recovery and multi-Issue soak

## Scope

Verify the complete M9–M12 durable loop under restart, duplicate, stale-identity, ambiguity and bounded parallel-Wave conditions. This Change adds verification and operator evidence only; it does not add workflow authority.

## Deterministic evidence

- File-backed restart fixture persists one active Wave with pending, claimed, provisioned, `delivery_pending` and `awaiting_result` stages, reopens the database and verifies exact IDs and status projection.
- Duplicate Lead action batches, scheduler claims and correlated result comments remain idempotent.
- Three-Issue scheduler soak proves Project WIP, Implementor and QA limits without starving an eligible role lane.
- Existing merge-Wave fixtures continue proving serialized Lead merge work and exactly one next-Wave planning Intent after all Issues are merged.
- Pause, Stop and project circuit breakers preserve ambiguous Intent, materialization and worker-result evidence.
- Service-worker recovery matrix covers pre-bootstrap, send-reserved, sent, target-observed and provisioned phases without blind bootstrap replay.
- Assignment claim ledger converts a reserved send to durable uncertain state after restart and refuses retargeting under the same claim identity.

## Safety boundaries

- No direct Dashboard merge.
- No arbitrary prompt injection.
- No ChatGPT response scraping.
- No weakening of exact-Head CI, fresh independent QA or merge read-back.
- Live smoke is disposable, bounded and must not reuse a production work lane.

## Required publication evidence

- full backend test and race suite;
- frontend tests/build;
- Chrome extension recovery suite;
- Runtime CLI and configuration validation;
- exact Candidate Head read-back;
- fresh independent QA before merge.
