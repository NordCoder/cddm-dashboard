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
6. Do not make speculative writes while the correct behavior is unresolved.
7. If a material product/architecture decision remains, stop with the smallest decision request that can unblock work.

Return only:

```text
STATUS: RESOLVED | BLOCKED | NO-DEFECT
FACTS: <concise evidence>
CONCLUSION: <root cause / contract interpretation>
NEXT: <bounded action or required decision>
```
