# Change — Adopt CDDM Codex Minimal 2.0 WebLead Worker Runtime

Milestone: process enablement before M6
Risk: Medium
Issue: #25

## Outcome

The repository supports WebLead-orchestrated Codex CLI workers that perform bounded reasoning/file edits/local verification in isolated worktrees while a deterministic host launcher owns Git metadata and GitHub delivery writes, eliminating Owner transport from the normal pipeline.

## Requirements

- `AGENTS.md` defines Owner, Web Lead, Worker, host launcher, GitHub, and exact-Candidate authority boundaries.
- Codex Workers run with `workspace-write`, `approval_policy = never`, and no responsibility for Git metadata or GitHub writes.
- Workers retain bounded read-only repository/GitHub inspection plus file-edit/test capability required for implementation.
- Project rules forbid Worker Git metadata/remote operations, GitHub writes/API calls, and credential access.
- Reusable prompts exist for shape, implement, investigate, fix-ci, and review without duplicating canonical context.
- `codex exec --output-last-message` returns one compact activity result to a host-owned path outside the Worker worktree.
- The trusted launcher verifies auth/canonical main, creates/reuses Change worktrees, invokes the correct model/activity, validates Worker result shape, performs host-side staging/commit, publishes only `change/<issue>` through the validated canonical repository URL, creates/updates PR state, and persists one result to GitHub.
- Review uses a fresh temporary detached worktree; fetched PR Head must equal the pre-resolved exact Head, the worktree must remain clean, and Head is rechecked before verdict persistence.
- M6 product implementation remains gated by explicit Owner Milestone approval.
- Existing product/runtime behavior and exact-Head CI semantics remain unchanged.

## Out of Scope

- M6 product implementation;
- automatic merge;
- Worker-controlled remote Git/GitHub writes;
- unattended product-scope changes;
- committed credentials;
- redesign of the product Supervisor protocol.

## Verification

- inspect changed surface for process/configuration only;
- validate project TOML and shell syntax;
- verify Worker prompts/skills require no staging/commit/push/GitHub persistence;
- verify host launcher refuses noncanonical main and stale PR review setup;
- verify host publication accepts no Worker-controlled remote/refspec;
- exact-Head CI remains green;
- fresh independent review confirms no Worker authority leak, stale review identity, or Owner-transport requirement.
