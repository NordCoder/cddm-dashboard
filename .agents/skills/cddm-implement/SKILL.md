---
name: cddm-implement
description: Execute one implementation-ready CDDM Codex Change in the current repository. Use for feature, bug-fix, refactor, or process implementation after material Design and dependencies are resolved.
---

# CDDM Implement

1. Read `AGENTS.md`, `.delivery/PRODUCT.md`, `.delivery/PRINCIPLES.md`, the Active Milestone, and the current Change contract/Issue.
2. Confirm the Change is implementation-ready: dependencies satisfied, material Design resolved, and current branch/worktree is not `main`.
3. Locate only relevant code, callers, tests, schemas, and configuration. Do not inventory the whole repository without evidence that it is necessary.
4. Implement the smallest maintainable solution inside Scope. Do not implement future Roadmap work.
5. Run the cheapest relevant V1 verifier after coherent edits and fix locally reproducible failures locally.
6. Before Candidate publication, run practical V2 checks from `AGENTS.md` for every affected surface and inspect the full diff.
7. Do not weaken tests, invariants, or policy to obtain green checks. Do not commit secrets or local environment data.
8. Commit and push the coherent Candidate on the current Change branch. Create or reuse the current draft PR and mark it ready only after Candidate-ready verification.
9. Post exactly one final `CDDM Worker Result` comment to the PR. Do not post progress comments.
10. Stop when Candidate Ready is satisfied. Do not perform unrelated cleanup.

Result schema:

```text
ACTIVITY: IMPLEMENT
STATUS: DONE | BLOCKED | NO-OP
HEAD: <exact Candidate sha or none>
CHANGED: <bounded summary>
VERIFY: <checks and results>
PR: <number or none>
BLOCKER: <only if material>
```
