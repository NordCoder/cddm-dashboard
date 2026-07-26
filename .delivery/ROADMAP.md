# CDDM Dashboard — Roadmap

## Completed

- M1 — Application Foundation — merged via PR #3.
- M2 — GitHub Supervisor Core — merged via PR #5.
- M3 — Workflow State and Routing — merged via PR #7.
- M4 — Prompt Planning and Policy — merged via PR #12.
- M5 — Responsive Web Dashboard — merged via PR #15.

## Active Milestone

### M6 — Browser Prompt Delivery

#### Owner Approval

Status: **Approved — 2026-07-26**.

The Owner approved the M6 Outcome, bounded Change set, dependency/parallel plan, key risks, and Exit Gate. Product implementation may proceed only through shaped Change Contracts under the WebLead 3.0 role boundary.

#### Outcome

A user can take a current policy-approved Prompt Plan, inspect the intended browser/chat binding, explicitly confirm delivery, and have the exact prompt sent once to the intended ChatGPT conversation without manual copy/paste and without the system reading ChatGPT responses.

#### Included

- backend-owned delivery command and lifecycle;
- browser worker identity and lane-to-chat binding;
- Chrome Manifest V3 command execution;
- dashboard confirmation and delivery status;
- stale, duplicate, restart, offline, and wrong-target protection;
- end-to-end installation and operating evidence.

#### Out of Scope

- reading or scraping ChatGPT responses;
- inferring worker completion from browser content;
- unattended autonomous dispatch;
- automatic merge;
- public multi-user authentication/deployment;
- Stage 7 PWA/mobile hardening.

#### Changes

| Change | Depends on | Risk | Current state |
| --- | --- | --- | --- |
| #17 — Delivery command contract | Stage 3 routing + Stage 4 planning; integrates #18 binding snapshot | High | Shaped — Ready for persistent implementation |
| #18 — Browser identity and lane binding | Stage 3 routing | High | Shaped — Ready for persistent implementation |
| #19 — Chrome extension execution | #17, #18 | High | Blocked |
| #20 — Confirmed delivery UX | #17; integrates #18 binding | High | Blocked |
| #21 — End-to-end hardening | #18, #19, #20 | High | Blocked |

#17 and #18 WHAT + material HARD HOW are fixed in their canonical Change Contracts. Shared semantics are reconciled before Worker execution: Stage 3 remains lane authority; #18 provides versioned binding + ephemeral presence proof; #17 snapshots that exact binding and uses a one-way at-most-once command lifecycle where a claimed command is never automatically requeued.

#### Parallel Plan

```text
Owner approval — complete
↓
Web Lead shaping:       #17 + #18 — complete
Lead reconciliation — complete
↓
Implementation Wave 1:  #17 + #18 persistent Codex sessions
Downstream Wave 2:      #19 + #20 after dependencies
Integration Wave 3:     #21
```

Wave membership is descriptive, not a lifecycle state. Unresolved HARD HOW is never delegated into parallel Worker decisions.

#### Key Risks

- duplicate or replayed consequential prompt delivery;
- stale Prompt Plan or Candidate context executing after repository state changes;
- incorrect or stale browser/chat binding targeting the wrong conversation;
- backend and extension independently defining incompatible delivery/binding semantics;
- browser restart/offline behavior causing silent replay or loss of operator visibility.

#### Exit Gate

M6 is accepted only when:

- current approved plan → explicit confirmation → exact delivery command → valid bound target → one prompt send works end to end;
- stale plan/Head/context cannot execute;
- duplicate polling/retry cannot produce duplicate send;
- missing, stale, closed, navigated, or offline target fails safely and visibly;
- backend/browser restart does not silently replay a completed consequential command;
- Project/lane isolation is demonstrated;
- Chrome extension does not read or persist ChatGPT response content;
- required local verification and exact-Head CI are complete;
- Web Lead QA accepts each required current Candidate and the integrated Outcome;
- additional independent review is complete where the Web Lead determines the residual risk warrants it;
- Owner accepts the integrated Milestone Outcome;
- manual Copy fallback remains usable unless a later approved Change explicitly removes it.

## Future

### M7 — Mobile Operation, Hardening, and v1.0 Pilot

Outcome: make the completed Supervisor and browser-delivery flow practical for private-network desktop/mobile operation, harden installation/recovery/observation, and complete the v1.0 pilot and release evidence.

Detailed M7 scope is intentionally deferred until M6 Acceptance.
