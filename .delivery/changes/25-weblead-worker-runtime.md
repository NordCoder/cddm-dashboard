# Change — Adopt CDDM Codex Minimal 2.0 WebLead Worker Runtime

Milestone: process enablement before M6
Risk: Medium
Issue: #25

## Outcome

The repository supports WebLead-orchestrated Codex CLI workers that perform bounded reasoning/file edits/local verification in isolated worktrees while a deterministic trusted host launcher owns Git metadata, GitHub context/writes, network-capable Candidate verification, and exact-Candidate persistence, eliminating Owner transport from the normal pipeline.

## Requirements

- `AGENTS.md` defines Owner, Web Lead, Worker, trusted host launcher, GitHub, and exact-Candidate authority boundaries.
- Codex Workers run with `workspace-write`, `approval_policy = never`, and shell network disabled.
- Workers receive bounded Issue/PR/CI evidence from the host and do not use `gh`, GitHub APIs, Git metadata writes, or remote Git operations.
- Host launch sanitizes GitHub credential-bearing environment variables/config for the Worker process.
- Reusable prompts exist for shape, implement, investigate, fix-ci, and review without duplicating canonical context.
- `codex exec --output-last-message` returns one strict activity result to a host-owned path outside the Worker worktree.
- The host launcher verifies auth/canonical origin/main, creates/reuses clean Change worktrees, invokes the correct model/activity, validates the full Worker result schema, and owns staging/commit/persistence according to the returned status.
- The host runs the full practical Candidate V2 for `IMPLEMENT: DONE` and `FIX_CI: FIXED` before any Candidate publication.
- Branch publication accepts no Worker-controlled remote/refspec and publishes only `change/<issue>` through the validated canonical repository URL.
- Candidate evidence is persisted only after GitHub PR Head is re-read and matches the published local Head.
- CI repair begins only from the exact current PR Head; all diagnoses are re-bound to that Head before persistence.
- Review uses a fresh temporary detached worktree; fetched PR Head must equal the pre-resolved exact Head, the worktree must remain clean, and Base/Head are rechecked before verdict persistence.
- M6 product implementation remains gated by explicit Owner Milestone approval.
- Existing product/runtime behavior and exact-Head CI semantics remain unchanged.

## Out of Scope

- M6 product implementation;
- automatic merge;
- Worker-controlled Git/GitHub writes;
- Worker shell-network access;
- force-push/rewrite workflows;
- unattended product-scope changes;
- committed credentials;
- redesign of the product Supervisor protocol.

## Verification

- inspect changed surface for process/configuration only;
- validate project TOML and shell syntax;
- verify Worker prompts/skills require no Git/GitHub persistence or network;
- verify host launcher refuses noncanonical main/origin and dirty/stale Change worktrees;
- verify `fix-ci` and review are bound to exact current Candidate identities before and after Worker execution;
- verify strict Worker result schemas before any persistence;
- verify full host Candidate V2 occurs before Candidate publication;
- exact-Head CI remains green;
- fresh independent review confirms no Worker credential/authority leak, stale Candidate/review evidence, or Owner-transport requirement.