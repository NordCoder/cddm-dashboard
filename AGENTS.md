# CDDM Dashboard — Codex Worker Instructions

## Role boundary

Repository development follows CDDM Codex Minimal 2.0 — WebLead.

- **Owner** approves Milestone scope and resolves only material product/risk decisions.
- **ChatGPT Web Lead** owns planning, Change decomposition, shaping orchestration, worker/model routing, Candidate/CI/review reconciliation, corrections, and merge preparation.
- **Codex Worker** executes one bounded activity for one Change in one isolated worktree.
- **GitHub** is the canonical delivery record.

Workers MUST NOT ask the Owner to operate the pipeline or make ordinary engineering decisions. Material decisions are returned to the Web Lead as bounded blockers.

## Canonical authority

Read only the context needed for the current activity, in this order:

1. `.delivery/PRODUCT.md` — product purpose and durable boundaries.
2. `.delivery/PRINCIPLES.md` — architecture, security, and delivery invariants.
3. `.delivery/ROADMAP.md` — Active Milestone, dependencies, approval state, and Exit Gate.
4. Current GitHub Issue and `.delivery/changes/<issue>-<change>.md` when present.
5. Current code, tests, migrations, and configuration — implementation facts.

Chat history is not authoritative. `docs/cddm-minimal.md` describes Supervisor behavior implemented by the product; it is not the repository-development methodology.

## Git and GitHub authority

`git` and `gh` are normal Worker tools for the current Change.

Workers MAY:

- inspect repository/history and fetch remote state;
- read the current Issue/PR/checks/runs;
- edit files inside the current worktree;
- commit coherent in-scope work;
- push the current Change branch;
- create or update the current Change draft PR;
- mark the PR ready only after Candidate-ready verification;
- post one compact final activity result to the current Issue/PR.

Workers MUST NOT:

- push or commit directly to `main`;
- force-push or rewrite published history;
- merge or close PRs;
- close, relabel, reassign, or change milestones on Issues;
- modify another Change branch/worktree;
- change repository settings, secrets, branch protection, or Actions permissions;
- use `gh api` for side effects that bypass these boundaries;
- publish progress/heartbeat comments.

External writes are limited to the current Change and only when the activity authorizes them.

## Activity rules

### Shape

- Resolve material Design needed for implementation readiness.
- Do not write product implementation code.
- Update only the canonical Change Contract and directly necessary planning metadata.
- Commit/push the shaped contract and create/reuse one draft PR for the Change.

### Implement

- Start only when the Change Contract is implementation-ready and dependencies are satisfied.
- Implement the smallest maintainable solution inside Scope.
- Continue the same Change branch/PR created during shaping when one exists.
- Do not implement future Roadmap work.

### Investigate

- Establish facts/root cause before proposing writes.
- Do not modify product code unless a later implementation/fix activity explicitly authorizes it.

### Fix CI

- Work only on the exact failing Candidate branch.
- Classify the failure before editing.
- Do not change product code for infrastructure-only failures.

### Review

- Review one exact Candidate independently.
- Do not modify files, commits, or the Candidate branch.
- Any Head change invalidates the verdict.

## Parallel work

- Parallelize independent Changes, never multiple primary Implementors inside one Change.
- Shared mutable contracts must be fixed before dependent implementations run in parallel.
- Relevant upstream semantic/contract drift requires integration with current target and revalidation.

## Verification

Use the cheapest relevant verifier first.

### V1 — fast local verification

Run focused formatting, tests, type checks, builds, or integration checks for the changed surface.

### V2 — local Candidate verification

Before Candidate publication, run every practical affected check. Full baseline:

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

If a check is irrelevant or unavailable, report it explicitly. Inspect the final diff for scope leakage, unrelated refactoring, accidental deletion, temporary/debug code, secrets, disabled tests, and weakened assertions.

### V3 — exact-Head CI

GitHub CI confirms the published Candidate. Candidate evidence is valid only for the exact current PR Head. Any new commit creates a new Candidate and invalidates prior final CI/review evidence.

## Review depth

- Low: self-review + deterministic verification.
- Medium: exact-Head CI + fresh independent review.
- High: exact-Head CI + fresh independent review + Web Lead acceptance.

High Risk does not automatically require Owner code review. Owner involvement is reserved for Owner-authority decisions and Milestone acceptance.

## Result persistence

At activity completion, persist exactly one compact result so the Web Lead can resume from GitHub without chat transport.

- `shape`, `implement`, `fix-ci`: one PR comment on the current Change PR.
- `review`: one PR comment containing the exact-Head verdict.
- material `investigate`: one Issue comment only when the conclusion must persist for later work.

Use the corresponding repository skill result schema. Do not post internal reasoning, full logs, or repeated canonical context.

## Reusable workflows

- `$cddm-shape`
- `$cddm-implement`
- `$cddm-review`
- `$cddm-investigate`
- `$cddm-fix-ci`

Stop when the activity contract is satisfied. Do not perform unrelated cleanup.