# CDDM Dashboard — Principles

## Product and architecture

- GitHub is the canonical external delivery-state source consumed by the Supervisor.
- Backend code owns lifecycle, routing, Candidate validity, blocker semantics, and policy authority.
- Frontend and browser components project or execute backend-authorized state; they do not create a competing workflow engine.
- OpenCode output is untrusted until deterministic policy validation succeeds.
- ChatGPT Web response content is never read or scraped.
- Consequential browser delivery requires explicit user confirmation by default.
- Automatic merge and unattended dispatch are outside the v1.0 default boundary.
- Credentials and authorization headers must not enter persisted product records, frontend storage, source control, or model context.

## Change delivery

- One Change has one observable Outcome, one primary Implementor, one branch/worktree, one primary PR, and one current Candidate.
- Future Roadmap work is planning context, not current Scope.
- Risk determines verification and review depth; Diff size alone does not.
- Local verification is the primary development feedback loop. CI independently confirms a published Candidate.
- Candidate evidence is exact-Head bound. A new Head invalidates prior final CI and review authority.
- Independent Ready Changes may execute in parallel only when dependencies and shared mutable contracts permit it.
- Shared contracts are fixed before dependent parallel implementations rely on them.
- WIP is bounded by integration and review capacity; agent count is not a delivery objective.
- Milestone completion requires integrated Outcome evidence, not merely merged Issues.
- A new process artifact, role, or gate is introduced only when it prevents a concrete failure class or shortens the critical path more than its coordination cost.
