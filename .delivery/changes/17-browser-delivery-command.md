# Change — Establish Browser Delivery Command Contract

Milestone: M6 — Browser Prompt Delivery
Risk: High
Issue: #17
Execution state: Shaped — Ready for persistent Change implementation after #18 binding contract is available for integration

## Outcome

Introduce the backend-owned delivery-command contract required to deliver one current policy-approved Prompt Plan to the currently valid bound browser/chat target after explicit user confirmation, without implementing Chrome DOM execution itself.

## Requirements

- A delivery command has stable backend identity and snapshots the exact Project/work-unit, Prompt Plan, route lane, Candidate Head/context and browser-binding identity authorized at confirmation time.
- Command creation is allowed only for a current dispatchable plan after explicit user confirmation.
- Stale plan, Head, context, route, binding or browser presence cannot create or claim an executable command.
- The browser receives the persisted canonical prompt; browser/frontend payloads cannot replace prompt text, routing, policy, Candidate identity or target identity.
- Duplicate confirmation requests for one confirmed intent cannot create duplicate commands.
- Polling, retries, Host/backend restart and lost acknowledgement cannot automatically re-arm a consequential send.
- Required command/audit state has deterministic SQLite restart semantics.
- Delivery result means only the extension's own compose/send action; it never implies that a ChatGPT response was read or accepted.

## Out of Scope

- browser worker registration and lane/chat binding persistence, owned by #18;
- Chrome extension DOM insertion/send implementation, owned by #19;
- ChatGPT response reading, scraping or completion inference;
- dashboard delivery UX, owned by #20;
- unattended dispatch;
- public/multi-user authentication;
- automatic retry of an uncertain or previously claimed consequential send.

## HARD HOW

### 1. Authority and source records

The backend remains the only delivery authority.

A command MAY be created only from a persisted planning generation when all are true at creation time:

- the generation belongs to the requested `project_id` and `issue_number`;
- a Prompt Plan exists;
- the plan is currently policy-valid: normal approved plans have generation status `approved`; deterministic fallback plans are allowed only when their final `policy_decision.status` is `approved`;
- current planning context still matches the plan `context_hash` and the current route still matches the plan action/target/lane;
- the route is dispatchable and has the same non-empty `lane_key` as the plan;
- `expected_head` equals the current authoritative Head value, including the legitimate empty value for a pre-Candidate initial route;
- #18 resolves that exact lane to one currently `ready` binding snapshot.

The confirmation request MUST identify an existing plan and expected identities. It MUST NOT accept replacement prompt text, replacement lane/role, replacement Head or replacement target data.

Stage 3 remains the authority that creates `lane_key`; this Change consumes it and MUST NOT implement a second router.

### 2. Confirmation and creation API

The backend owns a work-unit-scoped creation endpoint with semantics equivalent to:

`POST /api/projects/{project_id}/work-units/{issue_number}/deliveries`

The request contains only confirmation/CAS identity:

- `plan_id`;
- client-generated opaque `idempotency_key` for this explicit confirmation intent;
- `expected_plan_hash`;
- `expected_context_hash`;
- `expected_head`;
- `expected_lane_key`;
- `expected_binding_id`;
- `expected_binding_version`;
- `expected_presence_token` from #18.

The server re-reads authoritative planning/workflow/binding state and either creates one immutable command or returns a conflict/stale error. A stale browser view is never silently upgraded to newer plan, Head, lane, binding version or browser presence.

The command ID is backend-generated and globally opaque. The client idempotency key is scoped to the Project/work-unit confirmation API and is persisted with a uniqueness constraint.

Repeated creation with the same idempotency key and the same normalized confirmation fingerprint returns the existing command. Reuse of that key with different confirmation identities returns conflict and creates nothing.

### 3. Immutable command snapshot

A created command durably snapshots at minimum:

- command ID and confirmation idempotency key;
- Project ID, Issue number and planning generation/plan ID;
- plan hash, context hash and canonical prompt hash;
- exact canonical prompt text to execute;
- plan action, target role and lane key;
- expected Head;
- binding ID and binding version;
- worker ID;
- opaque #18 `presence_token` proving the browser session/target observation shown at confirmation time;
- normalized target kind/ref required by #19 to verify the current tab;
- created/expiry timestamps and lifecycle timestamps;
- claim identity and terminal outcome metadata when later present.

The snapshot is immutable except for lifecycle/claim/outcome fields. Rebinding, a newly generated plan, route movement or Head movement never rewrites an existing command.

### 4. Lifecycle

Canonical lifecycle:

```text
pending
  ├─> claimed ─> delivered
  │             ├─> failed
  │             └─> uncertain
  ├─> cancelled
  ├─> expired
  └─> invalidated
```

Legal meaning:

- `pending`: confirmed and persisted, but no browser execution right has been handed out;
- `claimed`: one browser claim has atomically consumed the command's single automatic execution right;
- `delivered`: #19 reports that it performed its own prompt insertion/send action for this claim;
- `failed`: #19 can positively assert the consequential send did not occur; this is still terminal and is not auto-retried;
- `uncertain`: the command was claimed but the system cannot prove whether the external send occurred;
- `cancelled`: user/backend cancellation before claim;
- `expired`: pending intent exceeded its bounded TTL before claim;
- `invalidated`: pending intent became stale because plan/context/Head/route/binding/presence no longer exactly matches.

No transition from `claimed` or any terminal state back to `pending` is legal.

Cancellation and ordinary expiry are legal only while `pending`. An already claimed command cannot be labelled safely cancelled/expired because the side effect may already have occurred.

### 5. At-most-once claim semantics

The backend provides a browser-worker claim operation, preferably one atomic `claim-next` operation rather than GET-then-claim race semantics.

A claim request carries:

- worker ID;
- current worker session identity required by #18;
- client-generated `claim_request_id`.

Before handing out the prompt, the backend atomically revalidates:

- command is still `pending` and unexpired;
- plan/context/Head/route identities are still current;
- the current #18 binding is `ready` and exactly matches binding ID/version, worker, target and `presence_token` stored in the command.

If any currentness check fails, the command becomes `invalidated`/`expired` as appropriate and is not returned for execution.

The successful transaction assigns one `claim_id`, changes `pending -> claimed`, persists claim timestamps/deadline, and only then returns the immutable execution payload.

The same `claim_request_id` is idempotent for the same claim. A different worker/session cannot acquire the command.

A claimed command is never automatically offered as a new execution attempt. If the claim outcome is lost, the system converges to `uncertain`, not to retry.

#19 MUST durably deduplicate its own execution by `claim_id` before performing the DOM send; backend idempotency alone cannot prove an external browser side effect happened only once.

### 6. Acknowledgement and uncertain outcome

The browser completes a claim with command ID + claim ID and one outcome:

- `delivered`;
- `failed` with bounded pre-send reason code/evidence;
- `uncertain`.

Duplicate identical completion is idempotent. A conflicting completion for an already terminal claim returns conflict without rewriting history.

If a claimed command exceeds its acknowledgement deadline, or browser/backend recovery cannot prove the outcome, deterministic reconciliation changes it to `uncertain`. It MUST NOT requeue it.

A user wishing to try again after `failed` or `uncertain` performs a new explicit confirmation, producing a new idempotency key and new command.

### 7. Freshness, restart and rebinding

Creation and claim both enforce currentness. Creation-time validity alone is insufficient.

#18 exposes a binding snapshot containing a stable binding revision plus an ephemeral `presence_token`. The token remains stable only while the same live browser session is observing the same usable target. Browser restart, navigation away/rebind, or backend liveness reset changes/removes the token.

Therefore an old pending command cannot silently execute after browser/session/target identity changes; it is invalidated at claim time and requires new confirmation.

SQLite persists commands, idempotency, claims and terminal outcomes. Backend restart MUST NOT turn `claimed`, `delivered`, `failed` or `uncertain` into `pending`.

On startup/reconciliation:

- overdue pending commands may become `expired`;
- claimed commands without a provable terminal acknowledgement may become `uncertain` after the configured claim deadline;
- terminal commands remain terminal.

### 8. TTL and deterministic time

Pending commands have a bounded backend-configured TTL with a default of 5 minutes. The value may be configuration, but semantics are fixed: expiry prevents claim and does not create a retry.

Claim acknowledgement has a separate bounded deadline. Tests use an injected clock; correctness MUST NOT depend on wall-clock sleeps.

### 9. Persistence and transactions

Delivery state is backend-owned SQLite state and must be stored in new delivery-owned tables/migrations rather than encoded in frontend state or GitHub comments.

Command creation, idempotency lookup/conflict detection, claim CAS and terminal acknowledgement each use transaction/uniqueness semantics sufficient to make concurrent duplicate requests deterministic.

Project deletion may cascade delivery records with the existing Project ownership boundary. Normal plan history mutation/deletion must not be required to execute a command because the immutable execution snapshot is stored on the command itself.

### 10. Safe disable and security boundary

A backend runtime/config kill switch disables new command creation and new claims while preserving read/audit visibility and existing terminal history. Disabling delivery never rewrites a claimed command as unsent.

M6 introduces no public authentication model. Browser worker IDs, session IDs, command IDs and presence tokens are routing/concurrency identities, not credentials. No GitHub token, ChatGPT cookie/token, backend secret or authorization header is persisted in command rows, returned to the dashboard unnecessarily, or included in prompts/model context.

Future M7 authentication may wrap these APIs without changing command identity or lifecycle semantics.

### 11. Compatibility boundary for #18, #19 and #20

#18 must provide a read-only resolution usable by delivery creation/claim with these semantic fields:

- binding ID/version;
- lane key;
- worker ID and current worker-session identity;
- target kind/ref;
- readiness;
- opaque current `presence_token`.

#19 consumes only a successfully claimed immutable execution payload and may not alter its routing/target/prompt identities.

#20 performs explicit confirmation by echoing the identities the user actually reviewed. It does not send prompt text as command authority and does not generate a hidden second confirmation after stale/conflict responses.

## Implementation Freedom

The Codex Worker may choose:

- package/file names and internal Go interfaces;
- exact SQLite table/index names and row-mapping helpers;
- UUID/ULID implementation for opaque IDs;
- HTTP response envelope shape and internal error types consistent with existing API style;
- reconciliation helper decomposition;
- test organization and fixtures;
- exact configuration variable names for TTL/kill switch within the fixed semantics above.

The Worker may NOT change lifecycle meaning, weaken currentness/CAS checks, add automatic retry after claim, accept client-supplied prompt/routing/target authority, or move routing/policy/binding authority into the browser/frontend.

## Verification

Required focused evidence includes:

- approved current plan creation and rejection of stale/rejected/planner-error/non-dispatchable plans;
- fallback generation accepted only with final approved policy decision;
- stale Head/context/lane and stale binding/presence rejection at both creation and claim;
- confirmation idempotency under sequential and concurrent duplicates, including same-key/different-input conflict;
- atomic concurrent claim proving one claim only;
- duplicate acknowledgement idempotency and conflicting acknowledgement rejection;
- lost acknowledgement/claim deadline converging to `uncertain` without requeue;
- restart tests proving claimed/terminal commands never return to pending;
- expiry/cancellation/invalidated transition tests;
- safe-disable behavior;
- Project/work-unit isolation;
- backend regression, `go test -race ./...`, exact-Head CI and Web Lead QA.

Because this is consequential High-risk state-machine work, fresh independent review is expected unless the Web Lead explicitly records why residual risk no longer warrants it.

## Dependencies

Product authority dependencies: Stage 3 routing and Stage 4 planning/policy are already merged.

Shared integration dependency: #18 binding semantics defined in `.delivery/changes/18-browser-lane-binding.md`. #17 may implement against a narrow read-only binding resolver/fake so the Change can execute in parallel; production wiring must remain fail-closed until #18 is available.

Operational dependency: WebLead 3.0 runtime merged through #27.

Owner approval: M6 approved on 2026-07-26.
