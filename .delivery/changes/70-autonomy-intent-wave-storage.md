# Change — Add Project Autonomy Profile and Durable Workflow Intent/Wave Storage

Milestone: M10 — Durable Orchestration
Issue: #70
Risk: High
Authorized Base: `70315420747cb15cb455658087867707f8214f1a`

## Outcome

Introduce the project-level continuous-autonomy configuration and durable Workflow Intent/Wave records required by CDDM Minimal v2.1 without scheduling or delivering autonomous work.

## Requirements

- Extend Project execution profiles with autonomy mode/state, Project Control Issue identity and bounded WIP settings.
- Preserve existing Projects as `manual_owner_dispatch` + `disabled` with v1 resource/protocol identities.
- Continuous mode requires `cddm-dashboard-resources/v2.0`, `cddm-minimal/v2.1`, `cddm-worker-result/v2`, a positive Control Issue and `auto_merge=false`.
- Persist Workflow Intents independently from Workflow Commands.
- Persist Waves and ordered Wave Issue membership.
- Enforce deterministic source-command/action idempotency and project isolation.
- Store only typed routing identities; arbitrary prompt text is prohibited.
- Project deletion cascades new records.

## HARD HOW

- Migration `0010` extends `project_execution_profiles` and creates `workflow_intents`, `workflow_waves` and `workflow_wave_issues`.
- The new `orchestration` package owns persistence types and invariants. It does not import `workerloop`.
- `workerloop.ProjectionService` remains the execution-profile API owner and validates exact profile combinations.
- Intent status and Wave status vocabularies are closed.
- C1 provides storage only. Result ingestion begins in #71; scheduling begins in #72.

## Out of Scope

- typed action ingestion;
- scheduler claims or lane leases;
- ChatGPT session provisioning;
- Workflow Command creation;
- browser delivery;
- automatic or direct merge.

## Verification

- migration and restart tests;
- v1 profile compatibility;
- continuous profile validation;
- intent/Wave idempotency, ordering and cascade tests;
- exact-Head CI and race detector;
- fresh independent QA.