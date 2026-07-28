# Change — Add Worker-Loop Controls and Pilot Readiness UX

Milestone: M7 — Worker Loop Integration / Pilot Readiness
Issue: #53
Risk: Medium
Authorized Base: current `main` after #52

## Outcome

Expose enough command, result, Candidate, role-binding and readiness state for an Owner to configure a Project and start a bounded pilot without manually copying prompts or worker reports.

## Requirements

- Work Unit view shows current route, attention, active Workflow Command, resource version, expected Head, delivery status and execution status.
- Show accepted worker result, GitHub comment identity, validation state and blocker classification.
- Show primary Candidate PR/Head, mergeability, exact-Head CI, QA-reviewed Head and approval freshness.
- Show protocol warnings without requiring raw JSON inspection.
- Store a small Project execution profile: methodology, resources, result protocol, delivery mode and QA session mode.
- Support `manual_fresh_binding` QA: Dashboard requires a newly bound QA conversation and retires the QA binding after terminal result.
- Add Project Pilot Readiness API/UI for synchronization, resources, planner, browser worker, Lead/Implementor lanes, QA mode, CI observability and protocol errors.
- Preserve reviewed delivery, per-Work-Unit auto-send and Manual Copy.

## Out of Scope

- automatic fresh-chat creation;
- full Owner Decision UX;
- public authentication;
- MISAK pilot execution.

## HARD HOW

- Backend remains authority for readiness and command/result state; frontend only projects it.
- Role bindings reuse the existing lane binding contract.
- Readiness is a diagnostic gate, not a new project lifecycle.
- Owner config never includes marker schemas, command taxonomy, Host internals or database identities.

## Verification

- frontend models, rendering and controller tests;
- readiness pass/fail fixtures;
- fresh QA binding and retirement tests;
- desktop/mobile regression;
- exact-Head CI and independent QA.
