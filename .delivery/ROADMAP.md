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
| #54 | Combined recovery fixtures, installation, configuration, operator guide, and final readiness evidence | Completed by the current integration Change |

## Pilot-ready outcome

```text
GitHub facts
→ Dashboard derives route
→ Dashboard creates Workflow Command
→ versioned role prompt reaches the exact bound ChatGPT chat
→ worker publishes a GitHub Issue comment
→ Dashboard validates cddm-worker-result/v1
→ Dashboard verifies consequential GitHub facts
→ Dashboard derives the next route
```

The local/private build is pilot-ready when the diagnostic endpoint reports `pilot_ready`. The integration does not execute `NordCoder/misak-website#140`.

## Explicitly out of scope

- reading or scraping ChatGPT responses;
- treating browser `delivered` as worker completion;
- automatic creation of a fresh ChatGPT conversation;
- automatic merge by default;
- public multi-user deployment without an authentication layer;
- hidden model authority over product decisions.

## Future

Future Changes may add authenticated remote deployment, notifications, automatic fresh-conversation provisioning, or explicitly approved merge automation. Each requires a new product/risk decision and Change Contract.
