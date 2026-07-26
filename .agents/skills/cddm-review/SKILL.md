---
name: cddm-review
description: Independently review one exact CDDM Codex Candidate. Use after a Candidate exists and requires Medium/High risk review; do not modify the Candidate during the review.
---

# CDDM Review

1. Establish the exact repository, Base, PR, and Head. If Candidate identity is ambiguous, return `EVIDENCE_INSUFFICIENT`.
2. Read the Active Milestone, current Change contract, exact diff, relevant implementation/tests, and Candidate-bound verification evidence.
3. Review independently; Implementor reasoning is not evidence.
4. Prioritize requirement gaps, semantic defects, regressions, failure paths, security, compatibility, persistence/concurrency correctness, test quality, and scope leakage.
5. Do not spend review effort on formatting or deterministic checks already covered by automation unless their evidence is missing.
6. Do not edit code in the review session.
7. Any Head change invalidates this verdict.

Approved output:

```text
VERDICT: APPROVED
HEAD: <sha>
FINDINGS: none
```

Finding output:

```text
VERDICT: BLOCKING_FINDINGS | EVIDENCE_INSUFFICIENT
HEAD: <sha>
FINDINGS:
- <severity> <location>: <precise defect>; impact: <material consequence>; required: <bounded disposition>
```
