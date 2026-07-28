# Change — Add Dashboard Resources v2 and Typed Worker Result Protocol

Milestone: M9 — Autonomous Contract
Issue: #67
Risk: High
Authorized Base: `60546860353b8ae474b0305b51f24af2dd6d4b43`
Depends on: #66

## Outcome

Add `cddm-dashboard-resources/v2.0` and strict `cddm-worker-result/v2` parsing while preserving active v1 package and result semantics.

## Requirements

- Add an independently loadable v2 resource package based on `cddm-minimal/v2.1`.
- Keep v1 as the runtime default until a later migration Change.
- Add fixed external Library attachment profiles and a closed typed-action vocabulary.
- Add Lead `actions_ready` and `merged` terminal results.
- Preserve Implementor and QA result semantics with v2 naming.
- Reject unknown fields, actions, duplicate action IDs, malformed identities, role/action mismatch and ambiguous Wave membership.
- Accept and persist v2 markers without executing action batches before M10.
- Derive terminal publication protocol identity from the selected resource package.
- Package repository and embedded copies byte-identically.

## Verification

- resource package loader tests for v1 and v2;
- v2 schema and attachment-profile validation;
- parser tests for valid/invalid role results and action batches;
- v1 compatibility regression;
- full backend tests and race detector;
- fresh independent QA.

## Out of scope

- Workflow Intent persistence;
- action materialization;
- scheduler and lane leases;
- session provisioning queue;
- automatic merge;
- Google Drive publication before exact-Head QA.
