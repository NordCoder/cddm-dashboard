# Change — Adopt CDDM Codex Minimal 2.0 WebLead Worker Runtime

Milestone: process enablement before M6
Risk: Medium
Issue: #25

## Outcome

The repository supports WebLead-orchestrated Codex CLI workers that execute bounded Change activities in isolated worktrees with local `git`/`gh`, persist compact results to GitHub, and do not require the Owner to operate the delivery pipeline.

## Requirements

- `AGENTS.md` defines Owner, Web Lead, Worker, GitHub, and exact-Candidate authority boundaries.
- Trusted workspace-write Workers have outbound network for bounded `git`/`gh` use.
- Project Codex rules permit routine inspection/current-Change delivery operations and forbid merge, force-push, and destructive repository actions.
- Reusable worker prompts exist for shape, implement, investigate, fix-ci, and review activities without duplicating canonical context.
- Repository skills persist one compact final activity result for Web Lead state reconstruction.
- A launcher creates/reuses isolated Change worktrees, checks authentication, selects a default activity model/effort, and starts `codex exec` with the matching prompt.
- Review uses a detached worktree at the exact PR Head.
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
- shell-parse `scripts/cddm-codex-worker.sh` and inspect worktree/model/prompt routing;
- verify prompts and skills agree on activity result contracts;
- exact-Head CI remains green;
- fresh independent review confirms no authority leak or Owner-transport requirement.
