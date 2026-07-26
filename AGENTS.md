# CDDM Dashboard — Codex Change Worker Instructions

## Role boundary

Repository development follows CDDM Codex Minimal 3.0 — WebLead.

- **Owner** owns WHY, Milestone approval, material authority decisions, and product acceptance.
- **ChatGPT Web Lead** owns WHAT, HARD HOW, Change shaping, dependency/parallelism decisions, model routing, Candidate/CI reconciliation, QA, and correction specification.
- **Codex Change Worker** owns the stable Change Goal, private Tasks, LITE HOW, implementation, tests, and local debugging inside one persistent Change session.
- **Trusted Host Runtime** owns worktrees, Codex thread persistence/rotation, deterministic V2, Git metadata, branch/PR publication, exact-Candidate persistence, and publication reconciliation.
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

One Change normally uses one persistent Codex thread across implementation and bounded corrections.

The Host prompt defines one deterministic stable Change Goal from the Issue identity and canonical Change Contract. The exact same Goal is used for initial implementation, resumes, and thread rotation. The persistent thread retains that Goal context; a rotated thread reconstructs the same Goal from the same canonical inputs.

If goal tools are available, the Worker SHOULD create/adopt that exact Goal. Goal-tool failure is not a delivery blocker.

The Goal remains stable across:

- initial implementation;
- implementation continuation;
- Web Lead QA corrections;
- implementation-related CI corrections;
- intentional thread rotation.

Bounded corrections become private Tasks/instructions under the same Goal; they do not replace WHAT or HARD HOW.

Codex Tasks are private execution state. The Worker MAY create, reorder, complete, or discard Tasks as useful and MUST NOT mirror them to GitHub.

A Goal MUST allow either valid stopping outcome:

1. the worktree is locally candidate-ready for Host V2; or
2. a material HARD HOW/dependency blocker has been identified and reported.

Do not loop indefinitely on blocked operations.

Persistent context is an optimization, not a dogma. The Host/Web Lead MAY rotate to a fresh thread in the same owned worktree when accumulated context becomes more expensive or misleading than reconstruction from the current contract/diff. Rotation recreates the same stable Goal and does not change canonical Change authority.

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
- retrieve GitHub credentials/tokens or host authentication files;
- modify another Change worktree;
- change repository settings, secrets, branch protection, or Actions permissions;
- publish progress/heartbeat state.

The Worker sandbox has no shell-network access. The Host launches Codex with a restricted permission profile, a non-login shell policy, a sanitized shell environment, and an isolated Worker `HOME`. The trusted Host supplies bounded GitHub context and owns networked delivery operations.

## Implementation loop

Use `$cddm-implement`.

Within the persistent session:

```text
approved contract
→ stable Goal
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

If V2 fails, no Candidate is published. The Web Lead may resume the same Change session with bounded failure evidence.

### V3 — exact-Head CI

GitHub CI independently confirms the published exact Candidate.

Any new commit creates a new Candidate and invalidates prior final CI/QA evidence.

## Candidate publication recovery

Candidate publication and PR bookkeeping are deterministic Host operations. The Host reconciles ambiguous push outcomes against the canonical remote and persists pending publication state before allowing another Codex turn.

If publication or PR bookkeeping is pending, the Host must reconcile that state first. The Worker is not rerun merely to repair transport/bookkeeping.

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

- `CANDIDATE_READY` — implementation is locally coherent and ready for Host V2.
- `CONTINUE` — useful progress exists but another turn is required.
- `BLOCKED` — a material dependency/HARD HOW conflict prevents safe progress.
- `NO_OP` — approved Change requires no file modification.

Do not add narrative outside the structured result.
