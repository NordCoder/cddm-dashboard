# Change — Add Confirmed Browser Delivery UX

Milestone: M6 — Browser Prompt Delivery
Risk: High
Issue: #20
Execution state: Shaped — Ready for persistent Change implementation

## Outcome

Extend the Stage 5 dashboard so the operator can inspect and explicitly manage the current lane-to-ChatGPT binding, review the exact current approved Prompt Plan and target identity, explicitly confirm one delivery intent, and observe the resulting #17 command lifecycle without manual clipboard transfer or frontend-owned authority.

## Requirements

- Consume current backend planning, workflow, #18 binding and #17 delivery APIs; do not duplicate routing/policy/currentness logic in the frontend.
- Expose browser delivery only for a current backend-projected dispatchable Prompt Plan.
- Show the exact currently observed binding target and readiness before confirmation.
- Provide explicit bind/rebind/disable controls using #18 version/lane CAS so M6 is operable without manual API calls.
- Consequential delivery requires a deliberate confirmation interaction distinct from ordinary navigation/copy actions.
- Confirmation echoes only the reviewed current CAS identities required by #17; frontend never submits replacement prompt/routing/target authority.
- One user confirmation intent has one stable client idempotency key; double-click/network retry cannot create a second command.
- A stale/conflict/unavailable response never silently refreshes and resubmits. The operator must review current state again and reconfirm.
- Render the full meaningful command lifecycle, including ambiguous/terminal states.
- Manual Copy remains available as a safe fallback.
- Do not read or display ChatGPT response content.

## Existing Backend Contract

The dashboard must use the merged backend surface:

Planning/workflow:

- `GET /api/projects/{project_id}/work-units/{issue}/state`;
- `GET /api/projects/{project_id}/work-units/{issue}/plans/latest`;
- existing planning history/context endpoints as already used by Stage 5.

Browser workers/binding:

- `GET /api/browser/workers`;
- `GET /api/projects/{project_id}/work-units/{issue}/browser-binding`;
- `PUT /api/projects/{project_id}/work-units/{issue}/browser-binding`;
- `DELETE /api/projects/{project_id}/work-units/{issue}/browser-binding`.

Delivery:

- `POST /api/projects/{project_id}/work-units/{issue}/deliveries`;
- `GET /api/projects/{project_id}/work-units/{issue}/deliveries`.

The frontend does not call browser `claim-next` or completion endpoints; those belong exclusively to #19.

## HARD HOW

### 1. Frontend authority boundary

Backend state is authoritative for:

- current work-unit route/action/lane;
- current Head/context;
- Prompt Plan status and final policy decision;
- binding identity/version/readiness/presence;
- delivery command creation and lifecycle.

The dashboard may derive presentation convenience only. It must not independently decide that a stale/rejected/non-dispatchable plan is safe merely because old UI data still exists.

Every consequential action echoes exact identities from the state the operator reviewed and relies on backend CAS/currentness validation.

### 2. Browser worker and binding presentation

On the work-unit/planning surface, show a Browser Delivery section with at least:

- current route lane key;
- current binding state (`ready`, `stale`, `unavailable`, `conflict`, `disabled`/none as projected by backend);
- binding ID/version when present;
- worker identity in a bounded diagnostic form;
- normalized target kind/ref suitable for the operator to distinguish the intended ChatGPT conversation;
- last-seen/staleness information exposed by #18;
- clear reason delivery is unavailable when binding is not ready.

Never display `presence_token` as a user-facing credential/value. It may remain in typed in-memory state only as the exact CAS field required for confirmation.

Do not expose cookies, browser auth data or arbitrary page content.

### 3. Binding management UX

M6 needs an explicit operator path to establish the #18 lane binding without raw API calls.

When the current work-unit route is browser-dispatchable:

- list only backend-projected live worker/current-target choices from `GET /api/browser/workers`;
- allow bind only to a currently observed supported target;
- initial bind sends current `expected_lane_key`, selected `worker_id`, exact normalized `target`, and no existing version;
- rebind sends the exact current binding version as CAS;
- disable/unbind sends current lane + current version;
- a version/lane/target conflict refreshes visible state but does not silently retry the mutation.

The frontend must not accept arbitrary free-text ChatGPT URLs as binding authority. Target choices come from backend-projected browser observations.

If a selected worker/target becomes stale between review and PUT, the backend rejection is surfaced and no alternative target is selected automatically.

### 4. Delivery eligibility presentation

A Deliver action is visible/enabled only when the currently loaded backend state demonstrates all presentation prerequisites:

- latest plan exists;
- generation is approved, or fallback generation has a final approved policy decision;
- plan action is `dispatch`;
- route/lane shown by current work-unit state is consistent with the plan;
- current binding projection is `ready`;
- plan/binding identities needed for #17 confirmation are available.

This frontend gating is defensive UX only; backend #17 remains authoritative and revalidates everything.

Rejected, planner-error, stale, owner-required/non-dispatchable or missing-plan states remain review/copy-only as appropriate and cannot expose a misleading active Deliver button.

### 5. Confirmation review

Consequential delivery requires a dedicated confirmation step/modal/panel after the user presses Deliver.

Before final confirmation show at minimum:

- plan summary/action/target role;
- exact expected Head (including explicit no-Candidate/empty value where legitimate);
- lane key;
- normalized bound ChatGPT target;
- binding readiness and version;
- a clear statement that the exact displayed/generated prompt will be sent to that ChatGPT conversation;
- no claim that ChatGPT response/completion will be read.

The prompt itself must remain inspectable through the existing Prompt Plan UI, and the confirmation surface should make it unambiguous which plan is being sent. Manual Copy remains adjacent/available.

### 6. Confirmation request and idempotency

For one final user confirmation action generate one opaque client `idempotency_key` and retain it for transport retries of that same confirmation intent.

Send exactly the current #17 confirmation shape:

- `plan_id`;
- `idempotency_key`;
- `expected_plan_hash` from the final policy decision/current plan;
- `expected_context_hash`;
- `expected_head`;
- `expected_lane_key`;
- `expected_binding_id`;
- `expected_binding_version`;
- `expected_presence_token`.

The frontend must never send prompt text, replacement target, replacement action/role or a newer hidden state in this request.

Disable duplicate submission while one request is in flight. Retrying the same unresolved network request uses the same idempotency key.

After a definitive conflict/stale/unavailable rejection, discard that confirmation intent: refresh current plan/binding state, require the operator to review it, then a later explicit confirmation gets a new idempotency key.

### 7. No hidden stale-state upgrade

If any action returns `409`, `503`, not-found or equivalent stale/unavailable result:

- surface a bounded human-readable reason;
- invalidate/refresh the affected local plan/binding view;
- do not automatically bind a replacement target;
- do not automatically regenerate/reselect a newer plan for sending;
- do not automatically submit a second delivery confirmation.

The operator must see the new current state before another consequential confirmation.

### 8. Delivery lifecycle rendering

After command creation, render the exact backend command identity and lifecycle from the deliveries list.

Support at least:

- `pending` — confirmed but not claimed;
- `claimed` — browser execution right consumed, outcome pending;
- `delivered` — extension reported its send action;
- `failed` — extension positively reported a pre-send failure;
- `uncertain` — send outcome cannot be proved and automatic retry is forbidden;
- `cancelled`;
- `expired`;
- `invalidated`.

Do not collapse `uncertain` into failed or offer an automatic Retry button. A new attempt requires a new explicit confirmation/current-state review under #17 semantics.

Show bounded outcome reason/evidence when backend returns it, treating it as diagnostics rather than ChatGPT response content.

Polling/refresh of delivery status is read-only and must not trigger confirmation or browser execution.

### 9. Current command attribution

Delivery history is scoped to the current Project/work-unit. Show enough immutable identity to distinguish attempts safely, such as command ID (abbreviated for display), creation time, plan ID, expected Head, target ref and status.

A terminal command remains historical even when a newer plan, Head or binding exists. Do not visually imply that old delivered/failed/uncertain records apply to the new current state.

### 10. Manual Copy fallback

Preserve existing Copy behavior for approved/fallback copyable plans.

Browser delivery is an additional explicit action, not a replacement for Copy in M6. Binding unavailable, extension offline or delivery kill-switch conditions must not remove the safe manual-copy path when the underlying plan is otherwise copyable.

### 11. Responsive and accessibility behavior

Preserve Stage 5 responsive behavior.

Consequential confirmation controls must remain usable on desktop and narrow/mobile viewport sizes already supported by the dashboard. Use semantic buttons/labels, keyboard-accessible confirmation/cancel flow, visible disabled/loading/error states and no color-only status meaning.

M7 PWA/offline/mobile installation behavior remains out of scope.

### 12. Typed API boundary

Extend the existing typed frontend API rather than issuing ad hoc fetches from individual components.

Add bounded types for:

- browser worker projection/current target;
- browser binding projection/readiness;
- binding create/rebind/disable inputs;
- delivery confirmation input;
- delivery command/list projection.

Unexpected/malformed backend shapes continue to fail visibly under existing typed API conventions rather than being treated as dispatch-ready defaults.

## Implementation Freedom

The Worker may choose:

- exact React/component decomposition and copy wording;
- whether Browser Delivery appears directly in the current planning view or as a closely associated work-unit panel;
- modal vs inline confirmation presentation;
- polling/refetch mechanism consistent with existing frontend architecture;
- visual treatment of target/readiness/lifecycle statuses;
- exact client UUID/idempotency implementation;
- test fixture organization.

The Worker may NOT move routing/policy/currentness authority into the frontend, invent target identities from free text, hide ambiguous `uncertain` state, auto-reconfirm after stale responses, or remove Manual Copy.

## Verification

Required focused evidence includes:

- ready binding presentation and clear stale/unavailable/conflict/disabled states;
- worker/current-target selection using backend projections only;
- bind/rebind/disable lane/version CAS and stale conflict handling;
- Deliver hidden/disabled for stale/rejected/planner-error/non-dispatchable/missing-binding conditions;
- approved and final-approved fallback confirmation eligibility;
- confirmation review displays exact plan/Head/lane/target identity;
- request contains only #17 confirmation/CAS fields, not prompt replacement authority;
- duplicate-click and transport-retry idempotency using one confirmation key;
- stale/conflict response requires fresh user review and does not auto-resubmit;
- lifecycle rendering for pending/claimed/delivered/failed/uncertain/cancelled/expired/invalidated;
- uncertain state has no automatic retry path;
- Project/work-unit delivery-history isolation;
- Manual Copy remains usable;
- typed API malformed/unavailable-state tests;
- responsive/accessibility regression coverage;
- existing Stage 5 planning tests and frontend production build;
- exact-Head CI and fresh Web Lead QA.

Because this Change exposes the consequential confirmation boundary, fresh independent review is expected unless the Web Lead explicitly records why residual risk no longer warrants it.

## Dependencies

#17 delivery command and #18 browser binding are merged and production-wired. #20 may execute in parallel with #19 because it consumes their stable backend contracts and does not depend on extension DOM implementation.

Canonical dependencies:

- `.delivery/changes/17-browser-delivery-command.md`;
- `.delivery/changes/18-browser-lane-binding.md`.

Owner approval: M6 approved on 2026-07-26.
