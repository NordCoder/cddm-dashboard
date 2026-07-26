---
name: cddm-implement
description: Execute one Ready CDDM Codex Change in the current repository. Use for feature, bug-fix, refactor, or process implementation after the Change scope and dependencies are defined.
---

# CDDM Implement

1. Read `AGENTS.md`, `.delivery/PRODUCT.md`, `.delivery/PRINCIPLES.md`, the Active Milestone in `.delivery/ROADMAP.md`, and the current Change contract/Issue.
2. Confirm the Change is Ready: required dependencies satisfied, no unresolved material ambiguity, and current worktree/branch is not `main`.
3. Locate only the relevant code, callers, tests, schemas, and configuration. Do not inventory the whole repository without evidence that it is necessary.
4. Implement the smallest maintainable solution inside Scope. Do not implement future Roadmap work.
5. During development, run the cheapest relevant V1 verifier after coherent edits and fix locally reproducible failures locally.
6. Before Candidate publication, run practical V2 checks from `AGENTS.md` for every affected surface and inspect the full diff.
7. Do not weaken tests, invariants, or policy to obtain green checks. Do not commit secrets or local environment data.
8. Stop when the Change Candidate Ready definition is satisfied. Do not perform unrelated cleanup.

Return only:

```text
STATUS: DONE | BLOCKED | NO-OP
CHANGED: <bounded summary>
VERIFY: <checks and results>
CANDIDATE: <Head/PR if published, otherwise not published>
BLOCKER: <only if material>
```
