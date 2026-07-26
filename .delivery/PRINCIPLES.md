# CDDM Dashboard — Principles

## Product and architecture

- GitHub is the canonical external delivery-state source consumed by the Supervisor.
- Backend code owns lifecycle, routing, Candidate validity, blocker semantics, and policy authority.
- Frontend and browser components project or execute backend-authorized state; they do not create a competing workflow engine.
- OpenCode output is untrusted until deterministic policy validation succeeds.
- ChatGPT Web response content is never read or scraped.
- Consequential browser delivery requires explicit user confirmation by default.
- Automatic merge and unattended product dispatch are outside the v1.0 default boundary.
- Credentials and authorization headers must not enter persisted product records, frontend storage, source control, or model context.

## Delivery authority

- The Owner governs WHY, Milestone approval, material product/risk authority, and product acceptance; the Owner does not operate the routine delivery pipeline.
- ChatGPT Web is the logical Lead and owns WHAT, HARD HOW, Change shaping, dependency/parallelism decisions, model routing, Candidate/CI reconciliation, QA, corrections, and merge preparation.
- Codex Change Workers own GOAL execution, private Tasks, LITE HOW, implementation, tests, and local debugging inside one persistent Change session.
- The trusted Host Runtime owns isolated worktrees, Codex thread/session persistence, bounded GitHub context, network-capable Candidate V2, Git commits, bounded branch publication, PR/Candidate persistence, and exact-Head transport.
- GitHub is the canonical delivery record; chat memory, Goals/Tasks, and narrative handoffs are not authoritative.
- One Change has one observable Outcome, one primary Implementor, one isolated worktree/branch, one persistent Codex thread, one primary PR, and one current Candidate.
- Future Roadmap work is planning context, not current Scope.
- The Web Lead fixes WHAT and material HARD HOW before product-code implementation. Workers may decide only LITE HOW inside explicit Implementation Freedom.
- A decision belongs to HARD HOW when another Change, shared contract, persistent state, security boundary, compatibility guarantee, or long-term ownership can materially depend on it.
- Local verification is the primary development feedback loop. Worker V1 precedes deterministic host V2; CI independently confirms the exact published Candidate.
- Candidate evidence is exact-Head bound. A new Head invalidates prior final CI and QA authority.
- QA is Web Lead-owned by default. Additional independent Codex review is risk-based, not universal.
- Implementation-related QA/CI corrections resume the same Change thread when the approved contract remains valid.
- Independent Ready Changes may execute in parallel only when dependencies are satisfied and HARD HOW/shared mutable contracts are already reconciled.
- WIP is bounded by integration and review capacity; agent count is not a delivery objective.
- GitHub writes are limited to persistent delivery facts and are performed by the trusted Host/Lead path.
- Milestone completion requires integrated Outcome evidence, not merely merged Issues.
- A new process artifact, role, or gate is introduced only when it prevents a concrete failure class or shortens the critical path more than its coordination cost.
