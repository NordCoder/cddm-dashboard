# Change — Establish Browser Identity and Lane Binding

Milestone: M6 — Browser Prompt Delivery
Risk: High
Issue: #18
Execution state: Planned — Ready for shaping after M6 Owner approval

## Outcome

Provide an explicit backend-owned binding between a deterministic workflow lane and the browser/chat target allowed to receive its prompts, with observable availability and safe stale-binding behavior.

## Requirements

- Browser worker instances have stable bounded identity suitable for local/private deployment.
- A deterministic workflow lane can be explicitly bound to one intended browser/chat target.
- Binding state is backend-owned and observable by dashboard and browser worker.
- Missing, stale, unavailable, or conflicting binding cannot be treated as dispatch-ready.
- Rebinding is explicit and cannot silently retarget an already-created consequential delivery intent.
- Binding data excludes ChatGPT response content and unnecessary credentials.
- Restart and offline behavior have deterministic semantics.

## Out of Scope

- delivery-command lifecycle;
- prompt insertion or send;
- ChatGPT response reading;
- autonomous target discovery;
- public multi-user identity/authentication.

## Design

Status: **Pending shaping.** Product-code implementation MUST NOT begin until M6 is Owner-approved and the canonical contract fixes the material decisions below.

Shaping must determine:

- browser worker identity and registration model;
- lane/binding key and target representation;
- liveness, stale, unavailable, and conflict semantics;
- explicit rebind behavior and interaction with existing delivery intents;
- persistence/restart boundary;
- minimum credential/secret exposure;
- API ownership consumed by dashboard and #19;
- rollback or safe-disable behavior.

The design must preserve Stage 3 lane routing authority rather than creating a second router.

## Verification

- binding create/read/update tests;
- missing/stale/conflict tests;
- restart/offline behavior where applicable;
- Project/lane isolation tests;
- backend regression and race suite;
- exact-Head CI;
- fresh independent review and Web Lead acceptance because Risk is High.

## Dependencies

Product dependencies: none.
Operational dependency: WebLead worker runtime #25 merged.
Approval dependency: M6 Owner approval.
Parallel-eligible with #17 for shaping after approval unless investigation discovers a shared mutable surface.
