---
name: cddm-implement
description: Implement or continue one WebLead-approved Change using the existing Change Contract. Use in a persistent Change session for initial implementation and bounded QA/CI corrections.
---

# CDDM Implement

1. Read `AGENTS.md`, the current Change Contract, host-supplied Issue/Lead instruction, and only relevant code/tests.
2. Treat the Change Contract as authoritative for WHAT and HARD HOW.
3. Own only LITE HOW: private implementation structure, local helpers/types, test organization, and bounded refactoring inside the approved architecture.
4. Use the stable Change Goal defined from the Issue and canonical Change Contract. If goal tools are available, create/adopt that exact Goal. Do not replace it for bounded QA/CI corrections or after thread rotation.
5. Maintain private Tasks under the stable Goal. Re-plan Tasks freely as implementation evolves; bounded Lead findings become Tasks, not new Goals. Never mirror Tasks to GitHub.
6. Implement the smallest maintainable solution inside Scope and run the cheapest relevant V1 checks while working.
7. If the approved HARD HOW conflicts with repository facts, stop and return `BLOCKED` with concise evidence rather than silently redesigning the system.
8. For resumed QA/CI corrections, change only what the Web Lead instruction requires unless evidence proves the contract itself is invalid.
9. Never stage, commit, push, use `gh`, or write GitHub state. The Host owns V2, Candidate publication/reconciliation, and PR persistence.
10. Stop at locally candidate-ready state; do not perform unrelated cleanup.

Return only the JSON object required by the host output schema.
