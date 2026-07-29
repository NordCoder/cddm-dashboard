# Change — Materialize Typed Lead Actions into Durable Workflow Intents

Milestone: M10 — Durable Orchestration
Issue: #71
Risk: High
Authorized Base: `654e2662425d4456edf98b6437ee3020c8d6b592`

## Outcome

Consume accepted `cddm-worker-result/v2` Lead `actions_ready` batches and atomically materialize them into durable Workflow Intents and Waves without scheduling or delivering them.

## Requirements

- Bind materialization to exact v2 Workflow Command/resource/result identities.
- Require the Lead command to target the configured Project Control Issue.
- Verify every action repository against the synchronized Project repository.
- Record one durable materialization outcome per GitHub result comment.
- Enabled continuous Projects may create Intents; manual/disabled/paused/stopped Projects preserve evidence without runnable work.
- Create optional Wave, ordered membership, Intents and materialization outcome atomically.
- Duplicate synchronization is idempotent.
- Mutated accepted evidence makes the materialization and its Intents ambiguous.
- `hold` and `owner_required` are blocking Intents.
- No arbitrary prompt text is persisted.

## HARD HOW

- Migration `0011` creates `workflow_materializations`.
- `orchestration.Materializer` implements the optional `workerloop.AcceptedResultMaterializer` boundary.
- Worker Result acceptance remains owned by `workerloop`; orchestration consumes only the stored/correlated result.
- Invalid action targets are persisted as blocked materialization outcomes, not partial batches.
- Infrastructure/storage failures remain errors and fail the synchronization pass closed.

## Out of Scope

- scheduler claims and lane leases;
- ChatGPT session provisioning;
- Workflow Command creation;
- browser prompt delivery;
- direct or automatic merge.

## Verification

- all six action types;
- Wave and no-Wave batches;
- duplicate sync/restart;
- profile state behavior;
- repository and Control Issue mismatch;
- atomic rollback/conflict;
- accepted-result mutation;
- v1 evidence-only behavior;
- exact-Head CI/race and fresh QA.