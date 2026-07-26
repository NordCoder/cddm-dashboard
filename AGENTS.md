# CDDM Dashboard — Codex Instructions

## Authority

For repository development, use the following canonical context in order:

1. `.delivery/PRODUCT.md` — product purpose and durable boundaries.
2. `.delivery/PRINCIPLES.md` — architecture, security, and delivery invariants.
3. `.delivery/ROADMAP.md` — Active Milestone, dependencies, and Exit Gate.
4. The current GitHub Issue and, for Medium/High work, `.delivery/changes/<issue>-<change>.md` when present.
5. Current code, tests, migrations, and configuration — implementation facts.

`docs/cddm-minimal.md` and the Supervisor event schemas describe workflow behavior implemented by the product. They are not the operating methodology for developing this repository. Do not remove or reinterpret that runtime protocol unless the current Change explicitly targets it.

## Change execution

- Work on one Change in one non-default branch/worktree with one primary Implementor.
- Never modify `main` directly.
- Read only the repository surfaces needed for the current Change; expand context when evidence requires it.
- High-risk work with unresolved material Design is Ready for shaping, not implementation. Shape the canonical Change Contract before product-code writes.
- Implement the smallest maintainable solution that satisfies the current Change and preserves the Active Milestone.
- Future Roadmap work is context, not Scope. Do not implement it speculatively.
- Resolve ordinary engineering choices from code, tests, conventions, and standard practice. Stop only for material ambiguity that changes Outcome, Scope, architecture, security, persistence semantics, compatibility, or authority.
- Do not weaken tests, policy checks, exact-Head semantics, or security boundaries to make verification pass.
- Never add credentials, tokens, local `.env` contents, or ChatGPT response data to source, fixtures, logs, or model-facing artifacts.

## Parallel work

- Parallelize independent Ready Changes, not one Change across multiple Implementors.
- Before parallel work, confirm no material dependency or competing ownership of a shared mutable contract.
- A shared contract must be fixed before dependent branches implement it in parallel.
- Relevant upstream contract or semantic drift requires integration with the current target followed by revalidation.

## Verification

Use the cheapest relevant verifier first.

### V1 — fast local verification

Run focused formatting, tests, type checks, builds, or integration checks for the surface just changed.

### V2 — local Candidate verification

Before publishing a Candidate, run every practical repository check affected by the Change. The current full baseline is:

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

If a check is irrelevant or unavailable locally, report that fact; do not claim it passed.

Inspect the final diff for scope leakage, unrelated refactoring, accidental deletion, temporary/debug code, secrets, disabled tests, and weakened assertions.

### V3 — Candidate verification

GitHub CI is an independent clean-environment gate, not the primary debugging loop. Candidate-bound evidence applies only to the exact current PR Head. Any new commit creates a new Candidate and invalidates prior final CI/review authority.

## Review and merge

- Low: self-review plus deterministic verification.
- Medium: fresh independent Codex review plus exact-Head CI.
- High: fresh independent review, exact-Head CI, and human review.
- Do not merge unless the user explicitly requests or the active Change grants merge authority.
- Do not transfer approval from one Head to another.

## Reusable workflows

Use repository skills when applicable:

- `$cddm-shape` — resolve a Medium/High Change contract and material Design before implementation.
- `$cddm-implement` — execute an implementation-ready Change.
- `$cddm-review` — independently review an exact Candidate.
- `$cddm-investigate` — resolve an uncertain defect or contract question without speculative writes.
- `$cddm-fix-ci` — diagnose a failed Candidate check and reproduce locally before correction.

## Output

Keep task output decision-oriented. For implementation work, default to:

```text
STATUS: DONE | BLOCKED | NO-OP
CHANGED: <bounded summary>
VERIFY: <checks and results>
CANDIDATE: <Head/PR if published, otherwise not published>
BLOCKER: <only when applicable>
```

Do not repeat the Issue, canonical documents, successful logs, or internal reasoning in the final report.
