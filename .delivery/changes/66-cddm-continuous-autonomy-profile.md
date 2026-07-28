# Change — Define CDDM Minimal Continuous Autonomy Profile

Milestone: M9 — Autonomous Contract
Issue: #66
Risk: High
Authorized Base: `0dc35ab26924b062d835c00661bd37309065a0a1`

## Outcome

Define `cddm-minimal/v2.1` as a bounded continuous-autonomy profile for Dashboard-operated delivery while preserving the exact-Candidate, independent-QA, CI, correction-cycle and merge-readback invariants of CDDM Minimal v2.0.

## Requirements

- Define `manual_owner_dispatch` and `continuous_dashboard_orchestration` execution modes.
- Add a normative Dashboard Orchestrator role that performs deterministic dispatch and queue control without product or architecture authority.
- Remove routine Owner prompt-transfer and wave-approval duties in continuous mode.
- Preserve Owner authority for material product, scope, architecture, visual and residual-risk decisions.
- Permit Lead to create and ready Issues, request supported worker actions, direct corrections, merge exact-approved Candidates and create subsequent waves within the approved product envelope.
- Introduce one Project Control Issue excluded from ordinary Implementor and QA routing.
- Define persistent Lead, fresh-per-command Implementor and fresh-per-exact-Head QA session policies.
- Serialize the Project Lead lane while allowing bounded per-Issue Implementor and QA concurrency.
- Define typed actions shared with `cddm-worker-result/v2`.
- Keep material instructions in GitHub Issues; typed actions are routing requests, not arbitrary prompts.
- Define Wave completion and continuous next-wave planning.
- Define pause, hold, owner-required, stale-evidence and fail-closed behavior.
- Define canonical `status:*` lifecycle mapping and repository-specific mapping rules.
- Preserve exact-Head QA freshness, CI gates, second-cycle Lead review, shared-surface controls, merge readback and `auto_merge=false` for the current Dashboard generation.

## Deliverables

- `docs/methodology/cddm-minimal-v2.1-continuous-autonomy.md`;
- Google Drive publication beside CDDM Minimal v2.0;
- action vocabulary consumed by M9-C2;
- adoption guidance for active v2.0 Issues.

## Verification

- consistency review against CDDM Minimal v2.0;
- no overlapping authority among Owner, Lead, Dashboard Orchestrator, Implementor and QA;
- action vocabulary consistency with Issue #67;
- fresh independent QA before integration.

## Out of scope

- scheduler implementation;
- session-provision queue implementation;
- direct Dashboard merge;
- arbitrary prose-to-action execution;
- ChatGPT response scraping;
- public multi-user operation.
