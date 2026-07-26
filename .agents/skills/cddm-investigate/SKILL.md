---
name: cddm-investigate
description: Investigate a defect, ambiguity, or unexpected repository/CI behavior before deciding whether code should change. Use when the root cause or correct contract is not yet established.
---

# CDDM Investigate

1. State the bounded question to resolve. Do not broaden into a general repository audit.
2. Read canonical Product/Principles/Milestone/Change context only when relevant to the question.
3. Gather facts in the cheapest order: current code/state → targeted search → relevant tests/logs → history/external sources only when needed.
4. Reproduce the problem locally when practical before proposing a correction.
5. Separate implementation defect, contract gap, stale documentation, infrastructure failure, and expected behavior.
6. Do not modify product or planning files during investigation.
7. If a material product/architecture decision remains, return the smallest decision request that can unblock the Web Lead.
8. Do not stage, commit, push, or write GitHub state; the host launcher persists the final result.

Final response, exactly:

```text
ACTIVITY: INVESTIGATE
STATUS: RESOLVED | BLOCKED | NO_DEFECT
FACTS: <concise evidence>
CONCLUSION: <root cause / contract interpretation>
NEXT: <bounded action or required decision>
```
