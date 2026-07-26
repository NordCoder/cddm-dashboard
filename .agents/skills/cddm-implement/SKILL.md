---
name: cddm-implement
description: Execute one implementation-ready CDDM Codex Change in the current repository. Use for feature, bug-fix, refactor, or process implementation after material Design and dependencies are resolved.
---

# CDDM Implement

1. Read `AGENTS.md`, `.delivery/PRODUCT.md`, `.delivery/PRINCIPLES.md`, the Active Milestone, host-supplied Issue context, and the current Change contract.
2. Confirm the Change is implementation-ready: dependencies satisfied, material Design resolved, and current worktree is the bounded Change worktree.
3. Locate only relevant code, callers, tests, schemas, and configuration. Do not inventory the whole repository without evidence that it is necessary.
4. Implement the smallest maintainable solution inside Scope. Do not implement future Roadmap work.
5. Run the cheapest relevant local verifier after coherent edits and fix locally reproducible failures locally.
6. Before returning `DONE`, run practical affected checks available without network access and inspect the full diff.
7. Do not weaken tests, invariants, or policy to obtain green checks. Do not add secrets or local environment data.
8. Do not stage, commit, push, or write GitHub state; the host launcher performs authoritative full V2 and creates/publishes the Candidate only after a successful result.
9. Stop when the implementation is locally coherent. Do not perform unrelated cleanup.

Final response, exactly:

```text
ACTIVITY: IMPLEMENT
STATUS: DONE | BLOCKED | NO-OP
CHANGED: <bounded summary>
VERIFY: <checks and results>
BLOCKER: <only if material; otherwise none>
```
