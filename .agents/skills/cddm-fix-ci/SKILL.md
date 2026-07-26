---
name: cddm-fix-ci
description: Diagnose and repair a failed exact-Candidate CI check under CDDM Codex. Use when a published Candidate has a failing or inconclusive required check.
---

# CDDM Fix CI

1. Treat the exact failing Candidate Head and bounded failing CI evidence supplied by the host as authoritative for this activity.
2. Classify the failure before editing: implementation defect, environment/infrastructure failure, flaky check, stale Candidate, or insufficient evidence.
3. Read only the relevant supplied diagnostic output first; expand into repository evidence only when necessary.
4. Reproduce the failure locally when practical using the same or closest repository command available without network access.
5. Change code only when evidence supports an implementation defect. Do not create a compensating code change for infrastructure failure.
6. After correction, run the cheapest relevant local checks available without network access.
7. Do not stage, commit, push, or write GitHub state; the host launcher rechecks the original PR Head, runs authoritative full V2, and publishes a new Candidate only when `STATUS: FIXED`.

Final response, exactly:

```text
ACTIVITY: FIX_CI
STATUS: FIXED | INFRA_FAILURE | BLOCKED | INCONCLUSIVE
CAUSE: <concise classification>
CHANGED: <bounded summary or none>
VERIFY: <local checks>
NEXT: <new exact-Head CI / retry / required decision>
```
