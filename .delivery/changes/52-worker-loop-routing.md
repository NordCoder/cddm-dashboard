# Change — Drive the Worker Loop from Resources and Accepted Results

Milestone: M7 — Worker Loop Integration / Pilot Readiness
Issue: #52
Risk: High
Authorized Base: current `main` after #51

## Outcome

Connect current Work Unit routing, Prompt Context, versioned role resources, Workflow Commands, Browser Prompt Delivery and accepted terminal markers into one bounded worker loop without reading ChatGPT responses or introducing a competing lifecycle.

## Requirements

- Render prompts as Dashboard Command Header + exact role resource + bounded current Prompt Context + terminal publication contract.
- Create one Workflow Command from a current policy-approved dispatchable Prompt Plan.
- Correlate the browser Delivery Command to the Workflow Command while preserving all existing plan/context/Head/lane/binding/presence checks.
- Browser `delivered` advances only to `awaiting_result`.
- Accepted marker results extend existing deterministic routing.
- Implementor `candidate_ready` requires independent PR existence and exact PR Head verification.
- QA results are effective only for the command's expected current Head and required exact-Head CI.
- Lead `ready_to_merge` runs a verification gate; it never causes blind merge.
- `blocked_inconclusive` with zero findings and `exact_candidate_ci_queued` keeps the Candidate, creates no correction cycle, waits for CI and then requests fresh QA on the same Head.
- Conflicting accepted results or unresolved external identity become manual Lead attention.
- No ChatGPT response-reading or semantic completion inference.

## Out of Scope

- automatic merge by default;
- automatic creation of ChatGPT conversations;
- complete Host operation bridge;
- general DAG scheduler or multiple Changes per Issue.

## HARD HOW

- Existing Work Unit derivation remains the only next-route authority.
- New command/result views are inputs to that derivation, not a second global state machine.
- GitHub synchronized facts override marker claims for PR, Head, CI, mergeability and Issue state.
- A changed Candidate invalidates prior QA by default.
- Workflow Command creation is idempotent for the same route/context/resource identity.
- Transport ambiguity is never converted into execution completion or automatic browser resend.

## Verification

- candidate-ready, QA correction, MISAK #149 process blocker, stale-QA and conflicting-result fixtures;
- browser delivery regression and restart behavior;
- exact-Head CI and independent QA.
