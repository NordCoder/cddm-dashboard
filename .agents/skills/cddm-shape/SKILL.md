---
name: cddm-shape
description: Shape or refine one Medium/High CDDM Codex Change before implementation. Use when Outcome, requirements, dependencies, risk, verification, or material Design decisions are not yet implementation-ready.
---

# CDDM Shape

1. Read `AGENTS.md`, Product, Principles, Active Milestone, the current Issue/Change Contract, and only the code/tests needed to understand the affected boundary.
2. Confirm one observable Outcome, explicit Requirements, Out of Scope, material dependencies, Risk, and Requirement-to-Verification mapping.
3. For High-risk work, resolve material Design decisions before implementation: ownership, contracts/schema, state transitions, failure semantics, persistence/compatibility, rollback or safe-disable behavior as applicable.
4. Distinguish engineering choices the Implementor may decide later from product/architecture decisions that belong in the contract.
5. Do not write product code during shaping. Update only the canonical Change/planning artifacts required to make execution unambiguous.
6. If evidence is insufficient, return the exact Decision/Discovery needed; do not invent semantics.
7. Mark the canonical Change Contract implementation-ready only when material ambiguity is resolved and dependencies permit execution.
8. Commit the shaped contract and publish the current Change branch only with `bash scripts/cddm-publish-branch.sh`. Create or reuse one draft PR for the Change.
9. Post exactly one final `CDDM Worker Result` comment to that PR. Do not post progress comments.

Result schema:

```text
ACTIVITY: SHAPE
STATUS: READY | DECISION_REQUIRED | DISCOVERY_REQUIRED
HEAD: <sha>
CONTRACT: <canonical path>
DECISIONS: <material decisions fixed>
DEPENDENCIES: <satisfied / remaining>
NEXT: <implementation or one bounded prerequisite>
```
