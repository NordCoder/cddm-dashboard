# CDDM Dashboard — Roadmap

## Completed foundation

- M1 — Application Foundation — PR #3.
- M2 — GitHub Supervisor Core — PR #5.
- M3 — Workflow State and Routing — PR #7.
- M4 — Prompt Planning and Policy — PR #12.
- M5 — Responsive Web Dashboard — PR #15.
- M6 — Browser Prompt Delivery, mobile workspace, auto-send, recovery, exact-target checks, and local security — PRs #48 and #49.

## Completed worker-loop integration

M7 extends browser transport into a GitHub-authoritative worker result loop through Changes #50–#54:

| Change | Outcome | State |
| --- | --- | --- |
| #50 | Versioned role resources and `cddm-worker-result/v1` schema | Merged via PR #55 |
| #51 | Durable Workflow Commands and Worker Result evidence | Merged via PR #56 |
| #52 | Prompt rendering, browser correlation, GitHub verification, and deterministic routing | Merged via PR #57 |
| #53 | Role bindings, execution surfaces, fresh-QA lifecycle, and Pilot Readiness | Merged via PR #58 |
| #54 | Combined recovery fixtures, installation, configuration, operator guide, and final readiness evidence | Merged via PR #59 |

## M9 — Autonomous Contract

M9 defines the authority and protocol required before Dashboard may operate a continuous project delivery loop:

| Change | Outcome | State |
| --- | --- | --- |
| #66 | `cddm-minimal/v2.1` continuous-autonomy profile, Project Control Issue, lanes, Waves and closed action vocabulary | Draft PR #68 — QA pending |
| #67 | `cddm-dashboard-resources/v2.0`, fixed attachment profiles and strict `cddm-worker-result/v2` compatibility | Stacked draft PR #69 — QA pending |

M9 deliberately does not execute typed Lead actions. Active v1 commands retain their original resource and result-protocol identity.

## Active — M10 Durable Orchestration

M10 is stacked on the exact M9-C2 Candidate and remains unmergeable until the M9 dependency chain is independently approved and integrated.

| Change | Outcome | State |
| --- | --- | --- |
| #70 | Project autonomy profile plus durable Workflow Intent and ordered Wave storage | Implementation |
| #71 | Atomic typed Lead action ingestion and fail-closed Intent materialization | Backlog — depends on #70 |
| #72 | Deterministic priority/WIP scheduler with durable lane leases | Backlog — depends on #71 |

M10 ends at a durable scheduler decision. It does not create ChatGPT sessions, attach Library files, create Workflow Commands, deliver browser prompts or merge code.

## Planned autonomous execution

1. **M11 — Browser Session Provisioning**
   - durable session-provision queue;
   - exact Library attachment resolution;
   - persistent Lead and fresh Implementor/QA policies;
   - operation without an open Dashboard page while Chrome and the extension remain available.
2. **M12 — Continuous Autopilot**
   - automatic command materialization;
   - Lead merge cycle and read-back;
   - operations UI, pause/stop and circuit breakers;
   - restart, duplicate, stale-Head and multi-Issue soak verification.

## Current worker-loop outcome

```text
GitHub facts
→ Dashboard derives route
→ Dashboard creates Workflow Command
→ versioned role prompt reaches the exact bound ChatGPT chat
→ worker publishes a GitHub Issue comment
→ Dashboard validates the command-bound result protocol
→ Dashboard verifies consequential GitHub facts
→ Dashboard derives the next route
```

The local/private v1 build remains pilot-ready when the diagnostic endpoint reports `pilot_ready`. M9/M10 additions remain inert unless the exact continuous profile is configured and the later materialization/scheduler Changes are present.

## Durable boundaries

- ChatGPT response content is not read or scraped.
- Browser `delivered` is transport evidence, not worker completion.
- Typed actions are routing requests, not arbitrary executable prompts.
- Dashboard does not invent product or architecture authority.
- Ambiguous browser sends, result conflicts and stale Candidate identities fail closed.
- Direct Dashboard merge remains disabled; `auto_merge=false`.
- Public multi-user deployment requires authentication and a separate product/risk decision.
