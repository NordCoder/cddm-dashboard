# Change — Establish Browser Delivery Command Contract

Milestone: M6 — Browser Prompt Delivery
Risk: High
Issue: #17

## Outcome

Introduce the backend-owned delivery-command contract required to deliver one current approved Prompt Plan to a browser worker after explicit user confirmation, without implementing browser execution itself.

## Requirements

- A delivery command has stable identity and carries enough context to prove Project/work-unit, Prompt Plan, lane, and expected Candidate Head.
- Command creation is allowed only for a current dispatchable plan after explicit user confirmation.
- Stale plan, Head, or context cannot create an executable command.
- Lifecycle covers bounded states required for polling, acknowledgement, failure, expiry/cancellation, and retry policy.
- Duplicate requests cannot create duplicate consequential delivery for the same confirmed intent.
- Required command/audit state has deterministic restart semantics.
- Browser execution cannot change routing, policy, or Candidate identity.

## Out of Scope

- browser/tab identity and binding;
- Chrome extension execution;
- ChatGPT DOM interaction or response reading;
- dashboard UX beyond backend compatibility required by the contract;
- unattended dispatch.

## Design

This Change owns a shared downstream contract. Command schema, idempotency identity, lifecycle, expiry/retry semantics, and persistence boundary must be explicit before #19/#20 rely on them.

## Verification

- current/stale plan creation tests;
- duplicate/idempotency tests;
- lifecycle transition tests;
- restart/persistence behavior where applicable;
- backend regression and race suite;
- exact-Head CI;
- fresh independent review plus human review because Risk is High.

## Dependencies

Product dependencies: none.
Operational dependency: bootstrap #16 merged.
Parallel-eligible with #18 unless investigation discovers a shared mutable surface.
