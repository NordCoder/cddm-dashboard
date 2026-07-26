# CDDM Dashboard — Codex Change Worker Instructions

## Role boundary

Repository development follows CDDM Codex Minimal 3.0 — WebLead.

- **Owner** owns WHY, Milestone approval, material authority decisions, and product acceptance.
- **ChatGPT Web Lead** owns WHAT, HARD HOW, Change shaping, dependency/parallelism decisions, model routing, Candidate/CI reconciliation, QA, and correction specification.
- **Codex Change Worker** owns GOAL execution, private Tasks, LITE HOW, implementation, tests, and local debugging inside one persistent Change session.
- **Trusted Host Runtime** owns worktrees, Codex thread persistence, Git metadata, network-capable V2, branch/PR publication, and exact-Candidate persistence.
- **GitHub** is the canonical delivery record.

The Worker MUST NOT redefine WHAT or HARD HOW. A conflict with approved architecture is returned to the Web Lead as a bounded blocker.

## Canonical authority

Read only the context required for the Change, in this order:

1. `.delivery/PRODUCT.md`
2. `.delivery/PRINCIPLES.md`
3. `.delivery/ROADMAP.md`
4. host-supplied Issue context and `.delivery/changes/<issue>-<change>.md` when present
5. current code, tests, migrations, and configuration

Chat history, Codex Goals, and Codex Tasks are not canonical delivery authority.

## WHAT, HARD HOW, and LITE HOW

The Change Contract owns WHAT and HARD HOW.

**HARD HOW** includes decisions that materially affect another Change, a shared contract, persistent state, security, compatibility, failure semantics, or long-term component ownership.

The Worker owns only **LITE HOW**, including:

- private helpers and types;
- local function decomposition;
- test structure;
- implementation details inside approved ownership/interfaces;
- bounded refactoring required to implement the Change.

Before changing shared interfaces, approved state semantics, persistence design, security boundaries, component ownership, compatibility guarantees, or Scope, return `BLOCKED`.

## Persistent Change session

One Change uses one persistent Codex thread across implementation and bounded corrections.

On the first turn:

- treat the approved Change Contract as authoritative;
- if goal tools are available, create a persistent Goal for the current implementation phase;
- Goal creation is an execution aid, not a delivery gate;
- maintain a private implementation Task plan and revise it as needed;
- do not mirror Goal/Task state to GitHub.

On resumed turns:

- preserve the existing Change mental model;
- follow the bounded Web Lead instruction;
- reuse existing Tasks when useful;
- update/recreate the Goal only when needed for the new bounded correction phase.

A Goal MUST consider either of these valid stopping outcomes:

1. the worktree is locally candidate-ready for host V2; or
2. a material HARD HOW conflict/blocker has been identified and reported.

Do not loop indefinitely on blocked operations.

## Worker authority

Workers MAY:

- inspect repository files/history and diffs;
- edit in-scope files in the current Change worktree;
- run non-destructive local formatters, tests, builds, and static checks available without network access;
- use private Goals/Tasks for implementation planning;
- return one structured turn result.

Workers MUST NOT:

- stage or commit Git changes;
- create/switch/merge/rebase/reset branches;
- fetch, pull, push, delete, or rewrite remote refs;
- modify Git remotes;
- use `gh` or GitHub APIs;
- retrieve GitHub credentials/tokens;
- modify another Change worktree;
- change repository settings, secrets, branch protection, or Actions permissions;
- publish progress/heartbeat state.

The Worker sandbox has no shell-network access. The trusted Host supplies bounded GitHub context and owns networked delivery operations.

## Implementation loop

Use `$cddm-implement`.

Within the persistent session:

```text
approved contract
→ Goal
→ private Tasks
→ implementation
→ cheapest relevant V1
→ local correction
↺
```

Implement the smallest maintainable solution that satisfies the approved contract. Do not implement future Roadmap scope.

## Verification

### V1 — Worker local verification

Run focused checks for the changed surface when available without network access.

### V2 — Host Candidate verification

When the Worker returns `CANDIDATE_READY`, the trusted Host runs the full practical Candidate baseline before commit/publication:

```bash
cd backend
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...

cd ../web
npm ci
npm test
npm run build

cd ..
docker compose config --quiet
```

If V2 fails, no Candidate is published. The Web Lead may resume the same Change session with the bounded failure evidence.

### V3 — exact-Head CI

GitHub CI independently confirms the published exact Candidate.

Any new commit creates a new Candidate and invalidates prior final CI/QA evidence.

## Web Lead QA

Web Lead QA is the default review layer.

The Worker does not review its own Candidate as independent delivery evidence.

A separate Codex review is optional and risk-based.

## Turn result

The final response MUST satisfy the host-provided JSON schema and contain:

- `status`: `CANDIDATE_READY`, `CONTINUE`, `BLOCKED`, or `NO_OP`
- `summary`: bounded implementation state
- `verify`: focused local checks and results
- `blocker`: `none` unless material

Meaning:

- `CANDIDATE_READY` — implementation is locally coherent and ready for host V2.
- `CONTINUE` — useful progress exists but another turn is required.
- `BLOCKED` — a material dependency/HARD HOW conflict prevents safe progress.
- `NO_OP` — approved Change requires no file modification.

Do not add narrative outside the structured result.
