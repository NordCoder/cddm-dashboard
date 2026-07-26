# CDDM Dashboard — Codex Worker Instructions

## Role boundary

Repository development follows CDDM Codex Minimal 2.0 — WebLead.

- **Owner** approves Milestone scope and resolves only material product/risk decisions.
- **ChatGPT Web Lead** owns planning, Change decomposition, shaping orchestration, worker/model routing, Candidate/CI/review reconciliation, corrections, and merge preparation.
- **Codex Worker** executes one bounded activity for one Change in one isolated worktree.
- **Host launcher** owns Git metadata writes and GitHub delivery writes after the Worker returns.
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

## Worker authority

Workers MAY:

- inspect repository files/history and local diffs;
- use read-only `gh` commands for the current Issue/PR/checks/runs;
- edit in-scope files inside the current worktree;
- run formatters, compilers, tests, builds, and other non-destructive local verification;
- return one compact activity result to the host launcher.

Workers MUST NOT:

- stage or commit Git changes;
- create/switch/merge/rebase/reset branches;
- fetch, push, delete, or rewrite remote refs;
- modify Git remotes;
- create/edit/comment/ready/merge/close PRs or Issues;
- use `gh api` or retrieve GitHub credentials/tokens;
- modify another Change worktree;
- change repository settings, secrets, branch protection, or Actions permissions;
- publish progress/heartbeat state.

The trusted host launcher performs staging, commits, bounded branch publication, PR state changes, and result persistence after the Worker exits successfully. This separation is mandatory; a Worker must not attempt to bypass it.

## Activity rules

### Shape

- Resolve material Design needed for implementation readiness.
- Do not write product implementation code.
- Update only the canonical Change Contract and directly necessary planning metadata.
- Return the SHAPE result; the host launcher persists the contract and draft PR.

### Implement

- Start only when the Change Contract is implementation-ready and dependencies are satisfied.
- Implement the smallest maintainable solution inside Scope.
- Continue the same Change worktree created during shaping when one exists.
- Do not implement future Roadmap work.
- Return the IMPLEMENT result; the host launcher creates the Candidate and publishes it.

### Investigate

- Establish facts/root cause before proposing changes.
- Do not modify product code.
- Return the INVESTIGATE result; the host launcher persists material evidence.

### Fix CI

- Work only on the exact failing Candidate worktree.
- Classify the failure before editing.
- Do not change product code for infrastructure-only failures.
- Return the FIX_CI result; the host launcher publishes a corrected Candidate only for `FIXED`.

### Review

- Review one exact Candidate independently.
- Do not modify files or delivery state.
- Any Head change invalidates the verdict.
- Return the REVIEW result; the host launcher rechecks exact Head before persistence.

## Parallel work

- Parallelize independent Changes, never multiple primary Implementors inside one Change.
- Shared mutable contracts must be fixed before dependent implementations run in parallel.
- Relevant upstream semantic/contract drift requires Lead reconciliation and revalidation.

## Verification

Use the cheapest relevant verifier first.

### V1 — fast local verification

Run focused formatting, tests, type checks, builds, or integration checks for the changed surface.

### V2 — local Candidate verification

Before returning `STATUS: DONE`, run every practical affected check. Full baseline:

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

GitHub CI confirms the Candidate created by the host launcher. Candidate evidence is valid only for the exact current PR Head. Any new commit creates a new Candidate and invalidates prior final CI/review evidence.

## Review depth

- Low: self-review + deterministic verification.
- Medium: exact-Head CI + fresh independent review.
- High: exact-Head CI + fresh independent review + Web Lead acceptance.

High Risk does not automatically require Owner code review. Owner involvement is reserved for Owner-authority decisions and Milestone acceptance.

## Result contract

The final response MUST be only the activity result schema defined by the selected repository skill: no prose before/after it, no code fence, no internal reasoning, and no GitHub write.

The host launcher captures this final response outside the Worker worktree, adds authoritative Git/PR metadata, and persists it to GitHub.

## Reusable workflows

- `$cddm-shape`
- `$cddm-implement`
- `$cddm-review`
- `$cddm-investigate`
- `$cddm-fix-ci`

Stop when the activity contract is satisfied. Do not perform unrelated cleanup.
