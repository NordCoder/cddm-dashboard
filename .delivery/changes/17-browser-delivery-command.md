# Change — Establish Browser Delivery Command Contract

Milestone: M6 — Browser Prompt Delivery
Risk: High
Issue: #17
Execution state: Planned — Ready for shaping after M6 Owner approval

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

Status: **Pending shaping.** Product-code implementation MUST NOT begin until M6 is Owner-approved and the canonical contract fixes the material decisions below.

Shaping must determine:

- command schema and stable/idempotency identity;
- lifecycle and legal state transitions;
- expiry, cancellation, retry, acknowledgement, and duplicate semantics;
- persistence/restart boundary;
- API ownership and authorization/confirmation boundary;
- compatibility boundary consumed by #19 and #20;
- rollback or safe-disable behavior.

## Verification

- current/stale plan creation tests;
- duplicate/idempotency tests;
- lifecycle transition tests;
- restart/persistence behavior where applicable;
- backend regression and race suite;
- exact-Head CI;
- fresh independent review and Web Lead acceptance because Risk is High.

## Dependencies

Product dependencies: none.
Operational dependency: WebLead worker runtime #25 merged.
Approval dependency: M6 Owner approval.
Parallel-eligible with #18 for shaping after approval unless investigation discovers a shared mutable surface.
