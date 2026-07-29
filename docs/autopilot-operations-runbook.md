# Continuous Autopilot operations runbook

## Purpose

Operate and diagnose the continuous Dashboard loop without creating new product authority. GitHub Issues, pull requests, exact Candidate Heads, CI and accepted worker-result comments remain authoritative.

## Normal startup

1. Confirm the Project uses `cddm-dashboard-resources/v2.0`, `cddm-minimal/v2.1` and `cddm-worker-result/v2`.
2. Confirm the Project ID, Project Control Issue and exact ChatGPT Project URL.
3. Confirm the latest GitHub synchronization is completed and healthy.
4. Inspect the active Wave, all durable Intents and leases, circuit breakers, ambiguous records and stale-Head warnings.
5. Enable Autopilot with the current operator revision.
6. Observe the ordered queue and the correlated provisioning, command, result and merge read-back records.

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
| Scheduler | Intent ID, action ID, role, lane key, durable lease ID, claim ID and lease status |
| Provisioning | Provisioning request ID, referenced lease ID, managed worker ID, managed session ID, exact tab ID, observed ChatGPT URL, binding ID and binding version |
| Command delivery | Materialization ID, referenced provisioning request ID, Workflow Command ID and status, delivery command ID and status, context hash and prompt hash |
| Worker result | GitHub result-comment ID, Workflow Command ID, Issue, role, result, payload hash, validation status/reason and accepted/observed timestamps |
| Merge read-back | Merge-cycle ID, referenced merge Intent, Issue, PR, approved Head, status and observed merge commit |

The lease token is a transition credential and is intentionally absent from the operator projection. Record the lease ID and claim ID instead. Every queue, lease, provisioning, command, result and merge record must resolve to the exact durable parent identities shown in the same response. If any authoritative collection, parent record or cross-link is absent or incompatible, treat the projection as malformed, keep work paused and investigate rather than substituting a default.

## Circuit-breaker recovery

Acknowledgement records operator awareness but does not unblock work. Resolve only after the listed recovery requirement and the complete correlated evidence chain are satisfied.

| Breaker family | Required recovery evidence |
| --- | --- |
| GitHub synchronization | Fresh completed healthy sync; no stale snapshot assumption |
| Missing exact-Head CI | Successful conclusive CI for the immutable Candidate Head |
| Stale Candidate or merge read-back | Fresh Issue/PR read-back matching the command-bound identity |
| Browser send uncertain | Durable delivery/ledger evidence; never retry blindly |
| Provisioning/session conflict | One healthy exact-tab worker, one managed session and one current binding for the lane |
| Library or Project scope mismatch | Exact ChatGPT Project and ordered attachment evidence |
| Ambiguous worker result | Correlated result-comment identity and validation status resolved against GitHub facts and the immutable Workflow Command |

## Restart interpretation

- `pending`: may be selected after restart if profile, WIP, lane and identity checks still pass.
- `claimed`: the durable lease remains authoritative until completed, released or expired.
- `surface_ready`: bootstrap phase decides recovery. `send_reserved` is terminal uncertain and is never resent automatically.
- `target_observed`: retry finalization against the same tab, target and attachment evidence; do not resend bootstrap.
- `delivery_pending` or `awaiting_result`: retain the same Intent, lease, provisioning request, managed session, binding, materialization, Workflow Command and delivery identities.
- `ambiguous`: preserve all evidence and trip the narrowest valid circuit breaker.

## Disposable live-smoke checklist

Use a temporary Issue, temporary branch, disposable ChatGPT conversation and a dedicated test lane. Do not use active production work.

1. Create the authoritative evidence-table row before changing control state. Record Project, Wave, GitHub target, scheduler, provisioning, binding, command and result/merge identities exactly as projected.
2. Confirm the exact ordered Library attachment profile and ChatGPT Project URL.
3. Start with Autopilot paused and zero active breaker in the test lane.
4. Resume and verify one Intent claim, one durable lease, one provisioning request and one managed exact-tab worker/session. Update the same evidence row.
5. Before bootstrap, restart the extension and verify the same provisioning request, worker, session, tab and binding are reused.
6. In a separate disposable attempt, interrupt after send reservation and verify terminal `uncertain` with no resend.
7. Verify target observation and finalization retain the exact Project conversation, tab, binding ID and binding version.
8. Deliver one assignment and verify its materialization ID, provisioning request ID, Workflow Command ID/status, delivery command ID/status, expected Head, context hash and prompt hash.
9. Restart during assignment delivery and verify the same ledger identity reports uncertain rather than sending again.
10. Publish one valid correlated result comment, synchronize GitHub and verify its comment ID, command ID, Issue, role and validation state against the original chain.
11. Repeat the same comment, action, claim and command identities; verify no duplicate command, lease, route or result record.
12. Introduce a stale Head in a separate disposable Intent; verify supersede/block without retargeting.
13. Trip a lane breaker, verify unrelated lanes continue, acknowledge it, and verify the lane remains blocked until the complete recovery row is rechecked.
14. For a merge attempt, record merge-cycle ID, referenced Intent, Issue, PR, approved Head and observed merge commit; reject any mismatched read-back.
15. Stop Autopilot and verify only safe pre-send pending records are superseded while durable evidence remains visible.
16. Delete or close disposable GitHub and ChatGPT artifacts only after the final projection still shows the same correlated identities and all uncertain work is resolved.

## Escalation

Stop and require owner review when GitHub facts conflict, the exact ChatGPT Project cannot be proven, attachment evidence drifts, a browser send is uncertain, result comments conflict, the projection omits an authoritative collection or identity, any projected record is orphaned, or merge read-back does not match the approved Head. Never infer success from ChatGPT response content.
