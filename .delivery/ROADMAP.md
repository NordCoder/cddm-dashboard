# CDDM Dashboard — Roadmap

## Completed

- M1 — Application Foundation — merged via PR #3.
- M2 — GitHub Supervisor Core — merged via PR #5.
- M3 — Workflow State and Routing — merged via PR #7.
- M4 — Prompt Planning and Policy — merged via PR #12.
- M5 — Responsive Web Dashboard — merged via PR #15.

## Active Milestone

### M6 — Browser Prompt Delivery

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

| Change | Depends on | Risk | Activation |
| --- | --- | --- | --- |
| #17 — Delivery command contract | process bootstrap #16 | High | Ready on bootstrap merge |
| #18 — Browser identity and lane binding | process bootstrap #16 | High | Ready on bootstrap merge |
| #19 — Chrome extension execution | #17, #18 | High | Blocked |
| #20 — Confirmed delivery UX | #17; integrates #18 binding | High | Blocked |
| #21 — End-to-end hardening | #18, #19, #20 | High | Blocked |

#### Parallel Plan

```text
Wave 1:  #17 + #18
Wave 2:  #19 + #20
Wave 3:  #21
```

Wave membership is descriptive, not a lifecycle state. A Change starts only when its material dependencies are satisfied and no shared-contract conflict is unresolved.

#### Exit Gate

M6 is accepted only when:

- current approved plan → explicit confirmation → exact delivery command → valid bound target → one prompt send works end to end;
- stale plan/Head/context cannot execute;
- duplicate polling/retry cannot produce duplicate send;
- missing, stale, closed, navigated, or offline target fails safely and visibly;
- backend/browser restart does not silently replay a completed consequential command;
- Project/lane isolation is demonstrated;
- Chrome extension does not read or persist ChatGPT response content;
- required local verification, exact-Head CI, independent safety review, and human acceptance are complete;
- manual Copy fallback remains usable unless a later approved Change explicitly removes it.

## Future

### M7 — Mobile Operation, Hardening, and v1.0 Pilot

Outcome: make the completed Supervisor and browser-delivery flow practical for private-network desktop/mobile operation, harden installation/recovery/observation, and complete the v1.0 pilot and release evidence.

Detailed M7 scope is intentionally deferred until M6 Acceptance.
