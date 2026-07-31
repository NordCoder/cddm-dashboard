# Continuous Autopilot operations runbook

## Purpose

Operate and diagnose the continuous Dashboard loop without creating new product authority. GitHub Issues, pull requests, exact Candidate Heads, CI and accepted worker-result comments remain authoritative.

## Normal startup

1. Confirm the Project uses `cddm-dashboard-resources/v2.0`, `cddm-minimal/v2.1` and `cddm-worker-result/v2`.
2. Confirm the Project ID, Project Control Issue and exact ChatGPT Project URL.
3. Confirm the latest GitHub synchronization is completed and healthy.
4. Inspect the active Wave, all durable Intents and leases, circuit breakers, ambiguous records and stale-Head warnings.
5. Enable Autopilot with the current operator revision.
6. Observe one evidence row for every Intent, including lease-only `claimed`, standalone `provisioned`, pending materialization, command and merge stages.

## Controls

- **Pause** stops new scheduler claims. Existing leases, managed sessions, commands and evidence remain durable.
- **Resume** requires the exact continuous profile, healthy completed GitHub synchronization and no unresolved breaker.
- **Stop** prevents future automatic work and supersedes only safe pre-send pending records. It must not erase or replay claimed, delivered, uncertain or ambiguous work.
- Every mutation carries the displayed operator revision. HTTP 409 means the projection is stale: refresh before deciding again.

## Authoritative evidence chain

Before recovery, breaker resolution, delivery retry decisions or cleanup, record one correlated row from a single current Autopilot projection. Do not combine values from different refreshes or infer a missing value.

| Scope | Required exact identities |
| --- | --- |
| Project and Wave | Project ID, repository, Control Issue, Wave ID and Wave source command ID |
| GitHub target | Issue number, PR number when present, exact Candidate Head and previous Head when present |
| Scheduler | Intent ID, action ID, role, lane key, durable lease ID, claim ID, owner, status, acquired time, expiry and release time when present |
| Provisioning | Provisioning request ID, referenced lease ID, managed worker ID, durable managed session ID, exact tab ID, observed ChatGPT URL, binding ID and binding version |
| Command delivery | Materialization ID, referenced provisioning request ID, Workflow Command ID and status, delivery command ID and status, context hash and prompt hash |
| Worker result | GitHub result-comment ID, Workflow Command ID, Issue, role, result, payload hash, validation status/reason and accepted/observed timestamps |
| Merge read-back | Merge-cycle ID, referenced merge Intent, Issue, PR, approved Head, status and observed merge commit |

The lease token is a transition credential and is intentionally absent from the operator projection. Record the lease ID and claim ID instead. `active_leases` is an exact filtered mirror: every authoritative active lease must appear exactly once, no non-active lease may appear, every non-secret field must match the authoritative record in `leases`, and the active count must agree. Claimed state is bidirectional with that mirror: every claimed Intent must have exactly one active lease, every active lease must belong to a claimed Intent, and one lane or Intent may not have two active leases. The queue must likewise contain each pending, blocked, claimed or ambiguous Intent exactly once and no terminal Intent. A `provisioned` record must carry its own durable managed session ID even when no materialization or delivery command exists. A pending materialization before delivery creation must continue exposing that same provisioning-owned worker/session/tab/binding chain. Every queue, lease, provisioning, command, result and merge record must resolve to the exact durable parent identities shown in the same response.

Workflow Command and delivery identities are Project-scoped even when their opaque IDs are globally shaped. A materialization must resolve both links inside its own Project. Do not accept a status or delivery row merely because another Project contains the same referenced ID. Once a delivery exists, compare its own worker, session and binding identity to provisioning; do not replace a conflicting delivery worker with the provisioning worker.

All displayed derived state is part of the same fail-closed read model. Pending, blocked and claimed Intent counts, provisioning and managed-session counts, active command and breaker counts, Lead-busy state and Project hold reason must agree with the authoritative arrays in the response. Queue waiting reasons must agree with the current autonomy state, blockers and active lanes. The displayed next action must follow from the same breakers, holds, leases, provisioning, commands, pending work and active Wave. The ambiguous count may include additional durable evidence not expanded into correlated rows, but it must never be smaller than the ambiguous Intents, commands and results that are shown; a failure to read the durable count is a projection failure, not zero. If any collection, parent record, required member, derived value or cross-link is absent, duplicated or incompatible, treat the projection as malformed, keep work paused and investigate rather than substituting a default.

Within one projection, active-Wave Issue numbers, materialization IDs, result-comment IDs, breaker IDs and merge-cycle IDs must be unique. Warnings that reference an Intent must repeat that Intent's Issue, PR and expected Head exactly.

## Provisioning commit interpretation

The managed session is process-local presence evidence until provisioning commits it durably. Finalization holds the exact browser-session snapshot stable through the entire SQL transaction and releases it only after commit or rollback.

- Do not treat a pre-transaction session lookup as durable proof.
- If a second session or conflicting target appears before finalization acquires the exact-session lock, finalization must fail with conflict.
- Registration and heartbeat for the same worker may resume after commit; the committed provisioning record remains bound to the session ID proven under the lock.
- A failed transaction must not leave a binding, session ID or provisioned status behind.

## Upgrade interpretation

Migration 18 introduces durable provisioning-owned session identity.

- A version-17 provisioned request linked to a materialized delivery is backfilled only when Project, worker, binding ID and binding version all match that exact delivery.
- A version-17 standalone or identity-conflicting provisioned request has no trustworthy durable source from which to recover the session identity. The migration preserves its evidence but changes it to terminal `uncertain` with reason `missing_durable_session_identity_after_upgrade`.
- Do not manually relabel such a quarantined record as `provisioned`, infer a session from a live tab, or reuse the old binding. Resolve it through the normal bounded recovery path.

## Circuit-breaker recovery

Acknowledgement records operator awareness but does not unblock work. Resolve only after the listed recovery requirement and the complete correlated evidence chain are satisfied.

| Breaker family | Required recovery evidence |
| --- | --- |
| GitHub synchronization | Fresh completed healthy sync; no stale snapshot assumption |
| Missing exact-Head CI | Successful conclusive CI for the immutable Candidate Head |
| Stale Candidate or merge read-back | Fresh Issue/PR read-back matching the command-bound identity |
| Browser send uncertain | Durable delivery/ledger evidence; never retry blindly |
| Provisioning/session conflict | One healthy exact-tab worker, one durable managed session and one current binding for the lane |
| Library or Project scope mismatch | Exact ChatGPT Project and ordered attachment evidence |
| Ambiguous worker result | Correlated result-comment identity and validation status resolved against GitHub facts and the immutable Workflow Command |

## Restart interpretation

- `pending`: may be selected after restart if profile, WIP, lane and identity checks still pass.
- `claimed`: the evidence row ends at the durable lease until provisioning begins; claim, owner and timestamps remain visible. A claimed Intent without exactly one active lease is malformed and must not be recovered by inference.
- `surface_ready`: bootstrap phase decides recovery. `send_reserved` is terminal uncertain and is never resent automatically.
- `provisioned`: the evidence row includes the provisioning-owned worker/session/tab/binding chain even before materialization exists.
- pending materialization: keep the same provisioning-owned session and binding visible even though Workflow Command or delivery identity may not exist yet.
- `target_observed`: retry finalization against the same tab, target and attachment evidence; do not resend bootstrap.
- `delivery_pending` or `awaiting_result`: retain the same Intent, lease, provisioning request, managed session, binding, materialization, Workflow Command and delivery identities.
- `ambiguous` or `uncertain`: preserve all evidence and trip the narrowest valid circuit breaker or operator recovery path.

## Disposable live-smoke checklist

Use a temporary Issue, temporary branch, disposable ChatGPT conversation and a dedicated test lane. Do not use active production work.

1. Create the authoritative evidence-table row before changing control state. Record Project, Wave, GitHub target, scheduler, provisioning, binding, command and result/merge identities exactly as projected.
2. Confirm the exact ordered Library attachment profile and ChatGPT Project URL.
3. Start with Autopilot paused and zero active breaker in the test lane.
4. Resume and verify the `claimed` row contains the exact Intent, lane, lease, claim, owner, acquisition and expiry fields before provisioning exists. Confirm there is exactly one active lease for that claimed Intent and lane.
5. Complete provisioning and verify the same row adds the durable worker session, tab and binding without requiring a command.
6. Create a pending materialization without delivering it and verify the command column retains the same worker/session/tab/binding evidence while Workflow Command and delivery IDs remain absent.
7. Before bootstrap, restart the extension and verify the same provisioning request, worker, session, tab and binding are reused.
8. In a separate disposable attempt, interrupt after send reservation and verify terminal `uncertain` with no resend.
9. Verify target observation and finalization retain the exact Project conversation, tab, binding ID and binding version.
10. Deliver one assignment and verify its materialization ID, provisioning request ID, Workflow Command ID/status, delivery command ID/status, expected Head, context hash and prompt hash. Confirm both command links resolve inside the current Project and that delivery worker/session/binding match provisioning.
11. Restart during assignment delivery and verify the same ledger identity reports uncertain rather than sending again.
12. Publish one valid correlated result comment, synchronize GitHub and verify its comment ID, command ID, Issue, role and validation state against the original chain.
13. Repeat the same comment, action, claim and command identities; verify no duplicate command, lease, route or result record.
14. Introduce a stale Head in a separate disposable Intent; verify supersede/block without retargeting.
15. Trip a lane breaker, verify unrelated lanes continue, acknowledge it, and verify the lane remains blocked until the complete recovery row is rechecked. Confirm the queue waiting reason and next action reflect the breaker rather than stale readiness text.
16. For a merge attempt, record merge-cycle ID, referenced Intent, Issue, PR, approved Head and observed merge commit; reject any mismatched read-back.
17. Stop Autopilot and verify only safe pre-send pending records are superseded while durable evidence remains visible.
18. Delete or close disposable GitHub and ChatGPT artifacts only after the final projection still shows the same correlated identities and all uncertain work is resolved.

## Escalation

Stop and require owner review when GitHub facts conflict, the exact ChatGPT Project cannot be proven, attachment evidence drifts, a browser send is uncertain, result comments conflict, the projection omits an authoritative collection or identity, a claimed Intent and active lease disagree, two active leases occupy one lane or Intent, a Workflow Command or delivery link resolves outside the current Project, any derived collection, metric, waiting reason or next action is incomplete or inconsistent, Lead/hold state conflicts with authoritative records, any projected record is orphaned or conflicts with its authoritative mirror, a pending materialization loses its provisioning-owned session evidence, an upgraded provisioning record lacks recoverable session identity, or merge read-back does not match the approved Head. Never infer success from ChatGPT response content.