# Change — Adopt CDDM Codex Minimal 2.0 WebLead Worker Runtime

Milestone: process enablement before M6
Risk: Medium
Issue: #25

## Outcome

The repository supports WebLead-orchestrated Codex CLI workers that execute bounded Change activities in isolated worktrees with local `git`/`gh`, persist compact results to GitHub, and do not require the Owner to operate the delivery pipeline.

## Requirements

- `AGENTS.md` defines Owner, Web Lead, Worker, GitHub, and exact-Candidate authority boundaries.
- Trusted workspace-write Workers have outbound network for bounded `git`/`gh` use.
- Project Codex rules permit routine inspection/current-Change delivery operations and forbid merge, direct `git push`, remote mutation, and destructive repository actions.
- Branch publication is performed only through a trusted repository helper that accepts no caller-controlled refspec and publishes only the current `change/<issue>` branch to the expected `NordCoder/cddm-dashboard` origin.
- Reusable worker prompts exist for shape, implement, investigate, fix-ci, and review activities without duplicating canonical context.
- Repository skills persist one compact final activity result for Web Lead state reconstruction.
- A launcher verifies authentication, requires a clean controlling `main`, refreshes remote state, proves local `main == origin/main`, creates/reuses isolated Change worktrees, and starts `codex exec` with the matching activity/model/effort.
- Every review uses a fresh temporary detached worktree at the exact current PR Head and removes it after completion.
- M6 product implementation remains gated by explicit Owner Milestone approval.
- Existing product/runtime behavior and exact-Head CI semantics remain unchanged.

## Out of Scope

- M6 product implementation;
- automatic merge;
- force-push/rewrite workflows;
- unattended product-scope changes;
- committed credentials;
- redesign of the product Supervisor protocol.

## Verification

- inspect changed surface for process/configuration only;
- validate `.codex/config.toml` and project rule syntax with current Codex tooling where available;
- shell-parse both worker runtime scripts;
- verify prompts and skills agree on activity result/publish contracts;
- verify branch publication cannot target `main`, arbitrary refs, or arbitrary remotes through caller arguments;
- exact-Head CI remains green;
- fresh independent review confirms no authority leak, stale review worktree, noncanonical-main source, or Owner-transport requirement.
