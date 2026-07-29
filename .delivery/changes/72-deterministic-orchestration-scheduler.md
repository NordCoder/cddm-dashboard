# Change — Add Deterministic Orchestration Scheduler and Lane Leases

Milestone: M10 — Durable Orchestration
Issue: #72
Risk: High
Authorized Base: `21f60d0179fe22d4df4f4d716a620ca9b938e6a9`

## Outcome

Select and durably claim runnable Workflow Intents under CDDM v2.1 priority, WIP and lane rules, producing typed scheduler decisions for M11 without creating sessions or Workflow Commands.

## Requirements

- Persist audit-safe lane leases with unique active lane and Intent ownership.
- Serialize the Project Lead lane.
- Serialize Implementor by Issue and QA by exact Candidate Head.
- Enforce Project WIP and parallel role limits.
- Deterministically order pending Intents by priority and durable identity.
- Respect project-global and Issue-scoped blocking Intents.
- Issue no claims for manual, disabled, paused or stopped Projects.
- Reconcile expired leases and make unfinished Intents pending again.
- Validate current synchronized GitHub Issue/PR/Head facts before claim.
- Supersede stale Intents instead of retargeting them.
- Validate lease owner/token for release, completion and supersession.

## HARD HOW

- Migration `0012` creates `workflow_lane_leases` with partial unique indexes for active lane and Intent ownership.
- `claim_id` is durable and unique per Project for idempotent caller retries.
- Scheduler claims at most one Intent per call inside one SQL transaction.
- Scheduler output contains Lease and typed Intent identities only.
- Priority numbers materialized by C2 are authoritative within the closed action vocabulary.

## Out of Scope

- ChatGPT session provisioning;
- Library attachments;
- Workflow Command creation;
- browser prompt delivery;
- direct or automatic merge;
- operations UI.

## Verification

- priority and stable tie ordering;
- Lead/Implementor/QA lane serialization;
- WIP/parallel limits;
- scope blocking;
- pause/stop/manual behavior;
- stale-Head supersession;
- lease expiry/release/restart;
- concurrent claims and race detector;
- exact-Head CI and fresh independent QA.