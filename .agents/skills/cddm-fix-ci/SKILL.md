---
name: cddm-fix-ci
description: Diagnose and repair a failed exact-Candidate CI check under CDDM Codex. Use when a published Candidate has a failing or inconclusive required check.
---

# CDDM Fix CI

1. Establish the exact failing Candidate Head and failing run/job/check. Do not diagnose logs from a different Head as current evidence.
2. Classify the failure before editing: implementation defect, environment/infrastructure failure, flaky check, stale Candidate, or insufficient evidence.
3. Read only the failing step and relevant diagnostic output first; expand logs when necessary.
4. Reproduce the failure locally when practical using the same or closest repository command.
5. Change code only when evidence supports an implementation defect. Do not create a compensating code change for infrastructure failure.
6. After correction, run the cheapest relevant V1 checks, then practical V2 Candidate verification.
7. A correction creates a new Candidate. Old CI and review authority are stale; publish and verify the new exact Head.

Return only:

```text
STATUS: FIXED | INFRA_FAILURE | BLOCKED | INCONCLUSIVE
CAUSE: <concise classification>
CHANGED: <bounded summary or none>
VERIFY: <local checks>
NEXT: <new Candidate CI / retry / required decision>
```
