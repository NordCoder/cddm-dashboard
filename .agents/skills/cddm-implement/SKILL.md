---
name: cddm-implement
description: Implement or continue one WebLead-approved Change using the existing Change Contract. Use in a persistent Change session for initial implementation and bounded QA/CI corrections.
---

# CDDM Implement

1. Read `AGENTS.md`, the current Change Contract, host-supplied Issue/Lead instruction, and only relevant code/tests.
2. Treat the Change Contract as authoritative for WHAT and HARD HOW.
3. Own only LITE HOW: private implementation structure, local helpers/types, test organization, and bounded refactoring inside the approved architecture.
4. On the first turn, create a Goal if goal tools are available. The Goal is optional execution assistance and MUST NOT replace the Change Contract.
5. Maintain a private Task plan. Do not persist or narrate Tasks to GitHub.
6. Implement the smallest maintainable solution inside Scope and run the cheapest relevant V1 checks while working.
7. If the approved HARD HOW conflicts with repository facts, stop and return `BLOCKED` with concise evidence rather than silently redesigning the system.
8. For resumed QA/CI corrections, change only what the Web Lead instruction requires unless evidence proves the contract itself is invalid.
9. Never stage, commit, push, use `gh`, or write GitHub state. The Host owns V2 and Candidate publication.
10. Stop at locally candidate-ready state; do not perform unrelated cleanup.

Return only the JSON object required by the host output schema.
