# Change — Adopt CDDM Codex Minimal 3.0 WebLead Runtime

Milestone: process enablement before M6
Risk: Medium
Issue: #25

## Outcome

The repository supports CDDM Codex Minimal 3.0: ChatGPT Web Lead owns WHAT, HARD HOW, orchestration, and QA; each approved Change executes through one persistent Codex CLI thread that owns one stable GOAL + private Tasks + LITE HOW; a deterministic Host Runtime owns worktrees, thread-id persistence/rotation, V2, Git/GitHub transport, exact-Candidate publication, and recovery.

## Requirements

- `AGENTS.md` defines the 3.0 Owner/Web Lead/Codex/Host authority boundary.
- Web Lead shaping and QA are defaults; dedicated Shape, Investigation, Fix-CI, and Review Worker roles are removed from the normal runtime.
- One Change maps to one persistent Codex thread across implementation and bounded QA/CI corrections.
- The Host establishes recoverable runtime ownership before the first Codex turn; a missing `thread.started` event cannot orphan the worktree.
- The Host captures `thread_id` from `codex exec --json` and keeps it only as ignored local runtime metadata under `.worktrees/`.
- Resume uses the stored `thread_id` and explicitly reapplies model/reasoning.
- Start, resume, and rotate use the same deterministic stable Change Goal derived from Issue identity + canonical Change Contract.
- Goal is execution orientation, never delivery authority; Goal-tool failure is not a blocker.
- QA/CI corrections become bounded Lead instructions/private Tasks under the same Goal rather than new Goals.
- Codex Tasks remain private Worker planning state and are never canonical or mirrored to GitHub.
- The Change Contract is authoritative for WHAT and HARD HOW; Worker autonomy is limited to LITE HOW/Implementation Freedom.
- Worker turns return strict JSON: `CANDIDATE_READY`, `CONTINUE`, `BLOCKED`, or `NO_OP`.
- `CONTINUE` preserves WIP and the same thread for a later resume.
- `CANDIDATE_READY` triggers deterministic Host V2 before commit/publication.
- V2 failure does not publish a Candidate; the same thread may be resumed with bounded failure evidence.
- Ambiguous push outcomes and post-push GitHub bookkeeping are reconciled by the Host against the canonical remote/exact Candidate.
- Pending Candidate reconciliation blocks subsequent Codex turns until deterministic delivery state is coherent.
- Candidate publication uses only the trusted Host Git/GitHub path and preserves exact-Head semantics.
- PRs remain draft until Web Lead QA/CI policy declares them merge-ready; Worker completion grants no ready/merge authority.
- M6 product implementation remains gated by explicit Owner Milestone approval.

## Architecture

### Ownership

```text
Owner    = WHY + authority
Web Lead = WHAT + HARD HOW + QA
Codex    = stable GOAL + private Tasks + LITE HOW + implementation
Host     = persistent thread/worktree + V2 + Git/GitHub + reconciliation
```

### Session State

Local ignored runtime state contains operational metadata only:

```text
issue
branch
worktree
thread_id
model
reasoning
contract
status
thread_turn_count
thread_generation
thread_history
candidate_head
pr
```

The stable Goal is deterministically reconstructed from Issue + Change Contract; it is not a second canonical artifact.

### Goal / Tasks

```text
Change Contract = WHAT + HARD HOW authority
Stable Goal     = durable Change execution orientation
Tasks           = private Worker execution plan
```

The stable Goal is identical across start/resume/rotate. Corrections are Tasks/instructions beneath it.

### Candidate Recovery

The Host reconciles pushed/pending Candidate state before any later Codex turn. Ambiguous transport results do not cause Worker re-execution or stale Candidate evidence.

### Correction Flow

```text
Candidate
→ CI / Web Lead QA
→ bounded finding
→ resume same thread
→ correction under same Goal
→ Host V2
→ new exact Candidate
```

A fresh Worker thread is used only when the existing context is misleading/uneconomical or the Change Contract is materially replaced.

## Out of Scope

- M6 product implementation;
- automatic merge;
- Worker-controlled Git/GitHub writes;
- Worker shell-network access;
- Goal/Tasks as canonical delivery state;
- mandatory separate Codex QA for every Change;
- Frontier/Sol as default shaping or implementation model;
- redesign of the product Supervisor protocol.

## Verification

- validate shell syntax, project TOML, and result JSON schema;
- verify runtime ownership survives missing initial `thread.started`;
- verify initial `codex exec --json` captures/persists `thread_id`;
- verify resume uses stored `thread_id` and reapplies model/reasoning;
- verify the same stable Goal is present in start/resume/rotate prompts;
- verify corrections remain Tasks/instructions under that Goal;
- verify dirty WIP stays inside the owned persistent Change session and is never published without `CANDIDATE_READY` + Host V2;
- verify V2 occurs before Candidate publication;
- verify ambiguous push/post-push failures are reconciled before another Codex turn;
- verify `CONTINUE` and `BLOCKED` do not create Candidates;
- verify Worker prompts assign WHAT/HARD HOW to Web Lead and only LITE HOW to Worker;
- verify no dedicated shape/investigate/fix-ci/review Worker path remains in the default harness;
- exact-Head CI remains green;
- fresh review confirms no stale-session/Candidate authority errors or Owner-transport requirement.
