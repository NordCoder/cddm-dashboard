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

- The Owner governs product intent, Milestone approval, material product/risk decisions, and product acceptance; the Owner does not operate the routine delivery pipeline.
- ChatGPT Web is the logical Lead and owns planning, dependency/parallelism decisions, shaping orchestration, worker/model routing, Candidate/CI/review reconciliation, corrections, and merge preparation.
- Codex Workers own bounded reasoning, file edits, and local verification; they do not own Git metadata or GitHub delivery writes.
- A deterministic host launcher owns worktree setup plus staging, commit, bounded branch publication, PR/result persistence, and exact-Head review handoff after Worker completion.
- GitHub is the canonical delivery record; chat memory and narrative handoffs are not authoritative.
- One Change has one observable Outcome, one primary Implementor, one isolated worktree/branch, one primary PR, and one current Candidate.
- Future Roadmap work is planning context, not current Scope.
- High-risk work with unresolved material Design is shaped before product-code implementation.
- Risk determines verification and review depth; Diff size alone does not.
- Local verification is the primary development feedback loop. CI independently confirms a published exact Candidate.
- Candidate evidence is exact-Head bound. A new Head invalidates prior final CI and review authority.
- Independent Ready Changes may execute in parallel only when dependencies and shared mutable contracts permit it.
- Shared contracts are fixed before dependent parallel implementations rely on them.
- WIP is bounded by integration and review capacity; agent count is not a delivery objective.
- High Risk requires stronger machine review and Lead acceptance, not automatic Owner code review.
- GitHub writes are limited to persistent delivery facts and are performed by the trusted host/Lead path, not by Worker narration.
- Milestone completion requires integrated Outcome evidence, not merely merged Issues.
- A new process artifact, role, or gate is introduced only when it prevents a concrete failure class or shortens the critical path more than its coordination cost.
