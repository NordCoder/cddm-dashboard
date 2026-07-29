# Change #83 — Lead merge read-back and continuous next-Wave loop

## Authorized Base

`027a18271f1843c5b19d42e16b2895ed19a47055` — exact M12-C1 Candidate from PR #99.

## Outcome

Close the continuous delivery loop after fresh QA approval without granting Dashboard direct merge authority. A typed `merge_candidate` Intent becomes one immutable Lead command in the exact provisioned Lead session. Dashboard accepts `merged` only after independent GitHub read-back, completes the exact Wave item, and creates one `plan_next_wave` Intent only when the complete ordered Wave is terminal.

## Authority and execution

- Dashboard never invokes a GitHub merge mutation.
- The M10 typed Intent remains the bounded merge/planning authority.
- The immutable Prompt Plan is derived from the current Stage-3 `dispatch → lead` route and exact Candidate Head.
- The Lead worker must use expected-Head protection and read back the consequential mutation before publishing `merged`.
- Browser `delivered` remains transport evidence only.
- A command-bound GitHub result plus independent Dashboard read-back is required for completion.

## Durable records

Migration `0016_merge_cycle_readback` adds:

- Wave-item status and verified merge commit evidence;
- one merge-cycle identity per Project/Intent and Workflow Command;
- exact repository, Issue, PR, approved Head and expected base branch frozen before execution;
- reported versus observed merge commit and verification timestamps;
- one `plan_next_wave` Intent per completed Wave;
- database-level activation gates that prevent browser claim before autonomous materialization and, for merge, before merge-cycle correlation.

## Result handling

### Verified merge

A `merged` result is accepted only when all identities agree:

- command, Project, repository, Issue and PR;
- approved Candidate Head;
- expected target branch;
- reported and observed merge commit;
- PR merged/closed state;
- terminal Issue state or lifecycle label.

The merge-cycle, Intent, lane lease, autonomous materialization and Wave item complete in one serializable transaction. The last terminal Wave item completes the Wave and creates exactly one pending Lead `plan_next_wave` Intent on the Project Control Issue.

### Eventual consistency

A valid result whose merged PR/Issue state is not visible yet remains `pending`. The accepted GitHub comment is replayed on later Supervisor synchronization and read-back is retried without replaying the merge mutation.

### Ambiguity or conflict

Wrong PR, Head, base branch, repository, Issue or merge commit marks the merge-cycle and materialization ambiguous, supersedes the exact lease, blocks the Wave and creates one Project-scoped hold. Dashboard does not guess, retarget or retry the consequential mutation.

### Typed action isolation

`actions_ready` is materialized before completing a legitimate next-Wave planning command. A typed merge command cannot materialize an action batch; it fails before partial Intents are created.

## Observation and recovery

Supervisor preserves the bounded open-Issue view and separately synchronizes recently closed Issues, allowing post-merge result comments to remain observable when GitHub auto-closes the linked Issue.

## Verification

Focused regressions cover:

- typed Lead merge and next-Wave Prompt Plan policy;
- exact Issue/PR GitHub read-back;
- browser claim activation before/after durable materialization and merge-cycle creation;
- `merge_not_visible` pending replay followed by verified completion;
- conflicting merge commit → Project hold;
- duplicate verified result idempotency;
- serialized two-Issue Lead lane;
- no next-Wave Intent after the first merge;
- exactly one next-Wave Intent after terminal multi-Issue Wave completion;
- migration schema, formatting, backend tests/race, frontend, extension and runtime/package validation.

## Explicit non-goals

- direct Dashboard merge API;
- arbitrary Lead prompt input;
- Owner approval for routine continuous-mode Waves;
- operations UI and circuit-breaker controls (#84);
- full restart/duplicate/stale-Head soak program (#85).
