---
name: cddm-review
description: Independently review one exact CDDM Codex Candidate. Use after a Candidate exists and requires Medium/High risk review; do not modify the Candidate during the review.
---

# CDDM Review

1. Establish the exact Base and Head supplied by the host plus the current repository state available in the detached review worktree.
2. Read the Active Milestone, current Change contract, exact diff, relevant implementation/tests, and Candidate-bound verification evidence.
3. Review independently; Implementor reasoning is not evidence.
4. Prioritize requirement gaps, semantic defects, regressions, failure paths, security, compatibility, persistence/concurrency correctness, test quality, and scope leakage.
5. Do not spend review effort on formatting or deterministic checks already covered by automation unless evidence is missing.
6. Do not modify files or delivery state.
7. Any Head change invalidates this verdict; the host launcher rechecks Head before persisting the result.
8. Do not write GitHub state; return only the verdict.

Final response, exactly:

```text
ACTIVITY: REVIEW
VERDICT: APPROVED | BLOCKING_FINDINGS | EVIDENCE_INSUFFICIENT
FINDINGS: none | <bounded findings with location, impact, and required disposition>
```
