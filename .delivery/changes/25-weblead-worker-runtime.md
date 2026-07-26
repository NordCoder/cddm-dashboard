# Change — Adopt CDDM Codex Minimal 3.0 WebLead Runtime

Milestone: process enablement before M6
Risk: Medium
Issue: #25

## Outcome

The repository supports CDDM Codex Minimal 3.0: ChatGPT Web Lead owns WHAT, HARD HOW, orchestration, and QA; each approved Change executes through one persistent Codex CLI thread that owns GOAL + private Tasks + LITE HOW; a deterministic Host Runtime owns worktrees, thread-id persistence, V2, Git/GitHub transport, and exact-Candidate publication.

## Requirements

- `AGENTS.md` defines the 3.0 Owner/Web Lead/Codex/Host authority boundary.
- Web Lead shaping and QA are the defaults; dedicated Shape, Investigation, Fix-CI, and Review Worker roles are removed from the normal runtime.
- One Change maps to one persistent Codex thread across implementation and bounded QA/CI corrections.
- The Host captures the initial `thread_id` from `codex exec --json` and persists it only as local runtime metadata under `.worktrees/`.
- Resume uses the stored `thread_id` and explicitly reapplies the stored model and reasoning effort.
- Goals are enabled as optional Worker execution assistance. In headless exec, the initial prompt explicitly asks the model to create the Goal when goal tools are available; `/goal` text is not used as a control-plane command.
- Codex Tasks remain private Worker planning state and are never canonical or mirrored to GitHub.
- The Change Contract is authoritative for WHAT and HARD HOW; Worker autonomy is limited to LITE HOW/Implementation Freedom.
- Worker turns return a strict JSON result: `CANDIDATE_READY`, `CONTINUE`, `BLOCKED`, or `NO_OP`.
- `CONTINUE` preserves uncommitted work and the same thread for a later resume.
- `CANDIDATE_READY` triggers full deterministic Host V2 before any commit/publication.
- V2 failure does not publish a Candidate; the same thread may be resumed with bounded failure evidence.
- Candidate publication uses only the trusted Host Git/GitHub path and keeps exact-Head semantics.
- PRs remain draft until Web Lead QA/CI policy decides they are merge-ready; Worker completion does not grant ready/merge authority.
- M6 product implementation remains gated by explicit Owner Milestone approval.

## Architecture

### Ownership

```text
Owner    = WHY + authority
Web Lead = WHAT + HARD HOW + QA
Codex    = GOAL + private Tasks + LITE HOW + implementation
Host     = persistent sessions + worktrees + V2 + Git/GitHub transport
```

### Session State

Local runtime state contains only operational metadata:

```text
issue
branch
worktree
thread_id
model
reasoning
contract
status
candidate_head
pr
```

It is not canonical delivery authority and MUST remain outside source control.

### Goal / Tasks

Goal is optional execution assistance. The approved Change Contract remains authoritative.

Tasks are private Worker planning state.

### Correction Flow

```text
Candidate
→ CI / Web Lead QA
→ bounded finding
→ resume same thread
→ correction
→ Host V2
→ new exact Candidate
```

A fresh Worker session is used only when the old session is invalid/misleading or the Change Contract is materially replaced.

## Out of Scope

- M6 product implementation;
- automatic merge;
- Worker-controlled Git/GitHub writes;
- Worker shell-network access;
- using Goal/Tasks as canonical delivery state;
- mandatory separate Codex QA for every Change;
- Frontier/Sol as default shaping or implementation model;
- redesign of the product Supervisor protocol.

## Verification

- validate shell syntax and project TOML;
- validate the result JSON schema;
- verify initial `codex exec --json` captures and persists `thread_id`;
- verify resume reads the stored thread and explicitly reapplies model/reasoning;
- verify dirty WIP is allowed only for the same owned persistent Change session and is never published without `CANDIDATE_READY` + Host V2;
- verify V2 occurs before commit/publication;
- verify `CONTINUE` and `BLOCKED` do not create Candidates;
- verify Worker prompts assign WHAT/HARD HOW to Web Lead and only LITE HOW to Worker;
- verify no dedicated shape/investigate/fix-ci/review Worker path remains in the default harness;
- exact-Head CI remains green;
- fresh review confirms no stale-session/Candidate authority errors or Owner-transport requirement.
