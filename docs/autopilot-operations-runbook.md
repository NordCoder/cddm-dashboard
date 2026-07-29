# Continuous Autopilot operations runbook

## Purpose

Operate and diagnose the continuous Dashboard loop without creating new product authority. GitHub Issues, pull requests, exact Candidate Heads, CI and accepted worker-result comments remain authoritative.

## Normal startup

1. Confirm the Project uses `cddm-dashboard-resources/v2.0`, `cddm-minimal/v2.1` and `cddm-worker-result/v2`.
2. Confirm the Project Control Issue and exact ChatGPT Project URL.
3. Confirm the latest GitHub synchronization is completed and healthy.
4. Inspect active circuit breakers, ambiguous records and stale-Head warnings.
5. Enable Autopilot with the current operator revision.
6. Observe the active Wave, ordered Intent queue, lane leases, provisioning records and immutable command IDs.

## Controls

- **Pause** stops new scheduler claims. Existing leases, managed sessions, commands and evidence remain durable.
- **Resume** requires the exact continuous profile, healthy completed GitHub synchronization and no unresolved breaker.
- **Stop** prevents future automatic work and supersedes only safe pre-send pending records. It must not erase or replay claimed, delivered, uncertain or ambiguous work.
- Every mutation carries the displayed operator revision. HTTP 409 means the projection is stale: refresh before deciding again.

## Circuit-breaker recovery

Acknowledgement records operator awareness but does not unblock work. Resolve only after the listed recovery requirement and exact GitHub evidence are satisfied.

| Breaker family | Required recovery evidence |
| --- | --- |
| GitHub synchronization | Fresh completed healthy sync; no stale snapshot assumption |
| Missing exact-Head CI | Successful conclusive CI for the immutable Candidate Head |
| Stale Candidate or merge read-back | Fresh Issue/PR read-back matching the command-bound identity |
| Browser send uncertain | Durable delivery/ledger evidence; never retry blindly |
| Provisioning/session conflict | One healthy exact-tab worker and one current binding for the lane |
| Library or Project scope mismatch | Exact ChatGPT Project and ordered attachment evidence |
| Ambiguous worker result | Resolve correlated result comments against GitHub facts and the immutable Workflow Command |

## Restart interpretation

- `pending`: may be selected after restart if profile, WIP, lane and identity checks still pass.
- `claimed`: the durable lease remains authoritative until completed, released or expired.
- `surface_ready`: bootstrap phase decides recovery. `send_reserved` is terminal uncertain and is never resent automatically.
- `target_observed`: retry finalization against the same tab, target and attachment evidence; do not resend bootstrap.
- `delivery_pending` or `awaiting_result`: retain the same Workflow Command and delivery identity. A reserved assignment send becomes uncertain after extension restart.
- `ambiguous`: preserve all evidence and trip the narrowest valid circuit breaker.

## Disposable live-smoke checklist

Use a temporary Issue, temporary branch, disposable ChatGPT conversation and a dedicated test lane. Do not use active production work.

1. Record repository, Control Issue, temporary Issue, PR, base ref and exact Candidate Head.
2. Confirm the current ChatGPT Project URL and exact ordered Library attachment profile.
3. Start with Autopilot paused and zero active breaker in the test lane.
4. Resume and verify one Intent claim, one lease, one provisioning request and one managed exact-tab worker.
5. Before bootstrap, restart the extension and verify the same tab is reused.
6. In a separate disposable attempt, interrupt after send reservation and verify terminal `uncertain` with no resend.
7. Verify target observation and finalization retain the exact Project conversation and binding version.
8. Deliver one assignment and verify its immutable command ID, expected Head and idempotency identity.
9. Restart during assignment delivery and verify the ledger reports uncertain rather than sending again.
10. Publish one valid correlated result comment, synchronize GitHub and verify the expected next route.
11. Repeat the same comment and claim identities; verify no duplicate command, lease or route.
12. Introduce a stale Head in a separate disposable Intent; verify supersede/block without retargeting.
13. Trip a lane breaker, verify unrelated lanes continue, acknowledge it, and verify the lane remains blocked until resolution.
14. Stop Autopilot and verify only safe pre-send pending records are superseded while durable evidence remains visible.
15. Delete or close all disposable GitHub and ChatGPT artifacts after recording exact-head evidence.

## Escalation

Stop and require owner review when GitHub facts conflict, the exact ChatGPT Project cannot be proven, attachment evidence drifts, a browser send is uncertain, result comments conflict, or merge read-back does not match the approved Head. Never infer success from ChatGPT response content.
