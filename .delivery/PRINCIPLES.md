# CDDM Dashboard — Principles

## Product and architecture

- GitHub is the canonical external delivery-state source consumed by the Supervisor.
- Backend code owns lifecycle, routing, Candidate validity, blocker semantics, correlation, external verification, and policy authority.
- Frontend and browser components project or execute backend-authorized state; they do not create a competing workflow engine.
- Browser Delivery Command, Workflow Command, Worker Result, and GitHub facts remain separate identities and lifecycles.
- A `cddm-worker-result/v1` marker is a bounded claim. Consequential transitions require current GitHub readback.
- OpenCode output is untrusted until deterministic policy validation succeeds.
- ChatGPT Web response content is never read, scraped, classified, or persisted.
- Consequential browser delivery requires explicit user confirmation by default. Opt-in auto-send changes transport review mode, not workflow authority.
- `auto_merge=false`; automatic merge is outside the pilot boundary.
- Credentials and authorization headers must not enter persisted product records, frontend storage, source control, or model context.
- Local/private loopback deployment is the supported pilot topology.

## Worker-loop authority

- A Workflow Command is created only from the current backend route, expected Head, resource version, and Prompt Context hash.
- A Browser Delivery Command proves only transport progress. `delivered` never means worker completion.
- One Workflow Command correlates to at most one Browser Delivery Command.
- One GitHub comment contains at most one result marker; missing `command_id` remains legacy/unbound evidence.
- Zero valid terminal results means `awaiting_result`; one valid terminal result may be accepted; conflicting valid results mean `ambiguous` and require Lead attention.
- QA approval is exact-reviewed-Head evidence. A changed PR Head invalidates prior approval.
- `manual_fresh_binding` QA mode retires the exact captured QA binding after terminal evidence and never retires a newer replacement version.
- Historical Prompt Plans are evidence, not current execution surfaces.

## Delivery authority

- The Owner governs WHY, material product/risk authority, and product acceptance; routine delivery does not require manual handoff copying.
- The Web Lead owns WHAT, HARD HOW, Change shaping, dependency decisions, Candidate/CI reconciliation, QA, corrections, and merge preparation.
- Implementor and QA workers publish terminal results to GitHub using the versioned result protocol.
- GitHub is the durable delivery record; chat memory and narrative reports are not authoritative.
- One Change has one observable Outcome, one primary PR, and one current Candidate.
- Candidate evidence is exact-Head bound. A new Head invalidates prior final CI and QA authority.
- Milestone completion requires integrated Outcome evidence, not merely merged Issues.
- A new process artifact, role, or gate is introduced only when it prevents a concrete failure class or shortens the critical path.
