# Change — Establish Browser Identity and Lane Binding

Milestone: M6 — Browser Prompt Delivery
Risk: High
Issue: #18
Execution state: Shaped — Ready for persistent Change implementation

## Outcome

Provide an explicit backend-owned binding between one deterministic Stage 3 workflow lane and one currently observable browser/ChatGPT target, with durable binding revision, ephemeral liveness proof and fail-closed stale/restart/rebind behavior.

## Requirements

- Browser worker instances have stable non-secret identity suitable for local/private deployment.
- One current workflow lane can be explicitly bound to one intended browser/chat target.
- Binding state is backend-owned and observable by dashboard, delivery service and browser worker.
- Missing, stale, unavailable, conflicting or superseded binding cannot be treated as dispatch-ready.
- Rebinding is explicit, compare-and-set protected, versioned and cannot silently retarget an existing delivery command.
- Browser/backend restart and navigation away from the target invalidate current liveness proof until the browser proves the target again.
- Binding/presence data contains no ChatGPT response content, cookies, auth tokens or other unnecessary credentials.
- Stage 3 remains the only routing authority; this Change consumes `lane_key` and does not invent a second router.

## Out of Scope

- delivery-command lifecycle and prompt snapshot, owned by #17;
- prompt insertion/send and browser DOM automation, owned by #19;
- dashboard confirmation UX, owned by #20;
- ChatGPT response reading/scraping;
- autonomous scanning/discovery of unrelated ChatGPT conversations;
- public multi-user identity/authentication;
- credential/token issuance for browser workers.

## HARD HOW

### 1. Browser worker identity

Each extension installation owns a stable opaque `worker_id` generated locally once and persisted in extension-local storage. It is not a credential and carries no authority by itself.

Each live browser/extension runtime also has an opaque `worker_session_id` generated on browser/extension runtime start. It is intentionally not durable across browser runtime restart.

The backend stores durable worker registration metadata keyed by `worker_id` but treats liveness/session state as runtime presence, not as durable authority.

A fresh session for the same worker ID may take over only when the previously observed session is no longer live/stale. Two concurrently live sessions presenting the same worker ID are a conflict and neither conflict state may become dispatch-ready until one session wins deterministically after staleness or explicit operator action.

Worker IDs/session IDs are routing/concurrency identities, not authentication secrets.

### 2. Registration and heartbeat

The browser API has semantics equivalent to:

- `POST /api/browser/workers/register`;
- `POST /api/browser/workers/{worker_id}/heartbeat`;
- `GET /api/browser/workers` for dashboard/diagnostic projection.

Registration/heartbeat payloads may contain only bounded identity/capability/presence data:

- worker ID;
- worker session ID;
- browser-worker protocol version/capabilities;
- current supported target observation or `null`;
- bounded timestamps supplied/validated by backend as appropriate.

The backend is authoritative for `last_seen` and readiness time.

Heartbeat does not scan or submit ChatGPT content. The extension may report only the current tab/target that it is already operating on or that the user explicitly selected for binding.

### 3. Target representation

M6 target kind is `chatgpt_conversation`.

The target reference is a normalized, query/fragment-free ChatGPT conversation identity sufficient for #19 to compare the current tab against the binding. It contains only:

- normalized allowed origin (`https://chatgpt.com` for the current v1 target);
- normalized conversation path/opaque conversation key derived from the URL;
- optional bounded display label that contains no page/response content.

Query strings, URL fragments, ChatGPT cookies/tokens, page text and response content are never persisted as target identity.

The normalization function is part of the shared browser/backend protocol: #18 and #19 must compare the same semantic target key. #19 may extend parsing for an additional explicitly supported ChatGPT URL form only if the normalized identity remains equivalent and no response content is read.

### 4. Ephemeral presence proof

A durable binding is not sufficient for dispatch readiness. The backend also maintains a current presence observation for the worker/session/target.

For one continuously live `(worker_id, worker_session_id, target_ref)` observation the backend exposes an opaque `presence_token`. The token is stable while that exact live session continues to report the same usable target and changes/disappears when any of these occur:

- browser/extension session restarts;
- current target navigates away or becomes unusable;
- a different target is reported;
- backend runtime restarts and loses prior live-presence state;
- the observation becomes stale;
- worker-session conflict is detected.

`presence_token` is a concurrency/freshness proof, not a credential.

This deliberate ephemeral boundary means a persisted binding survives restart, but delivery requires fresh post-restart presence proof. Old pending commands that snapshot an earlier token fail closed under #17 instead of being silently delivered later.

### 5. Liveness and readiness

Worker/target liveness uses a backend-configured bounded TTL with a default of 30 seconds and an injected clock in tests.

A binding is `ready` only when all are true:

- binding is enabled;
- its Project/lane ownership is valid;
- exactly one live non-conflicting worker session exists for the bound worker ID;
- that session has a fresh heartbeat;
- its current target normalizes exactly to the bound target;
- a current presence token exists.

Projection states must distinguish at least:

- `ready`;
- `stale` — heartbeat/presence exceeded TTL;
- `unavailable` — worker is live but not currently observing the bound target or target is unusable;
- `conflict` — worker identity/session or exclusive target ownership is ambiguous;
- `disabled` — binding was explicitly disabled/unbound.

No non-ready state may be treated as dispatchable by #17/#20.

### 6. Lane authority and binding key

The durable binding key is `(project_id, lane_key)`.

`lane_key` MUST be consumed from current Stage 3 `workflow.Route`; #18 does not independently derive routing semantics.

The work-unit binding mutation API is scoped to a Project/work unit and includes `expected_lane_key`. The backend re-reads current workflow state and refuses binding if the supplied lane is not the current lane owned by that Project/work unit or the current route has no safe browser-dispatch lane.

This preserves the existing Stage 3 format/authority and prevents clients from binding arbitrary strings to targets.

### 7. One lane, one target; one target, one lane

At any time:

- one `(project_id, lane_key)` has at most one enabled current binding;
- one live `(worker_id, target_ref)` may be the enabled target of at most one lane across the local control plane.

The second rule intentionally prevents two workflow lanes from sharing one ChatGPT conversation and cross-contaminating execution context.

Attempts to bind a target already owned by another enabled lane return conflict. The backend never silently steals/reassigns it.

An operator must explicitly disable/rebind the previous lane before the target can move.

### 8. Binding identity, revision and CAS

Each lane binding has a stable opaque `binding_id` and monotonic integer `binding_version`.

Initial bind creates version 1. Any material mutation increments version, including:

- worker change;
- target change;
- explicit disable/unbind;
- explicit re-enable/rebind.

Mutation requests include the caller's expected version (`null`/zero only for first create). Stale version returns conflict without mutation.

The binding projection returned to #17/#20 includes at minimum:

- binding ID/version;
- project ID and lane key;
- worker ID;
- normalized target kind/ref;
- enabled state;
- derived readiness state;
- current worker session ID when live;
- current opaque presence token when ready;
- backend-derived last-seen/staleness metadata suitable for UI display.

### 9. Binding API

The backend owns APIs with semantics equivalent to:

- `GET /api/projects/{project_id}/work-units/{issue_number}/browser-binding` — current route lane plus current binding/readiness projection;
- `PUT /api/projects/{project_id}/work-units/{issue_number}/browser-binding` — explicit create/rebind using `expected_lane_key`, worker ID, normalized target identity and expected binding version;
- `DELETE /api/projects/{project_id}/work-units/{issue_number}/browser-binding` or equivalent explicit disable operation with expected lane/version;
- worker register/heartbeat/list endpoints described above.

A PUT is accepted only if the selected worker currently proves the selected target with fresh non-conflicting presence. It cannot create a ready binding to an offline/unobserved target.

DELETE/disable is a versioned durable mutation, not destructive audit erasure. The implementation may update the current row in place while retaining sufficient timestamps/version identity; a full historical binding event table is optional.

### 10. Rebinding and existing delivery commands

Rebinding changes the binding version and current target but never rewrites a #17 delivery command.

#17 snapshots binding ID/version, worker ID, target ref and presence token at confirmation. Therefore:

- an unclaimed command referring to an older version/presence becomes `invalidated` when creation/claim currentness is checked;
- a claimed command is not retargeted or automatically retried after rebind;
- a delivered/failed/uncertain command remains immutable audit history.

There is no "follow latest binding" behavior for consequential commands.

### 11. Restart semantics

Durable SQLite state:

- worker registration identity/metadata;
- current lane binding ID/version/worker/target/enabled state;
- binding timestamps required for audit/projection.

Ephemeral runtime state:

- live worker session ownership;
- heartbeat freshness;
- current target observation;
- presence token.

After backend restart all bindings initially project non-ready until a worker/session reports fresh presence again. Newly established presence gets a new token; old delivery intents cannot silently resume against it.

After browser restart the extension retains `worker_id`, creates a new `worker_session_id`, re-registers/heartbeats, and must re-prove the current target before the binding can become ready.

### 12. Security and credential boundary

M6 does not introduce a public auth system. The private/local deployment trust boundary remains unchanged until M7.

The following MUST NOT be stored in worker/binding rows or exposed to dashboard state:

- GitHub credentials;
- ChatGPT cookies, session tokens or authorization headers;
- backend secrets;
- arbitrary page text;
- ChatGPT prompts/responses except prompt text already owned by #17 delivery state.

Extension/browser APIs use worker/session/presence identities only for routing/freshness and MUST NOT present them as security credentials.

### 13. Safe disable

A backend runtime/config kill switch may disable browser binding readiness globally while preserving durable binding records for inspection. In disabled mode no binding is reported ready and #17 cannot create/claim browser delivery commands.

An individual binding can also be explicitly disabled with version CAS. Re-enabling/rebinding increments version and requires fresh worker/target presence.

### 14. Shared compatibility boundary

#17 receives a read-only binding resolution with these fixed semantics:

- exact lane key;
- binding ID/version;
- worker ID/current session identity;
- target kind/ref;
- readiness;
- current presence token.

#17 is responsible for snapshotting these values into a command and revalidating them before claim.

#19 is responsible for generating/persisting stable worker ID, generating fresh session ID, normalizing the supported ChatGPT target, heartbeating presence and checking the claimed target before DOM execution.

#20 may display/select only backend-projected workers/targets/bindings. It must not invent a target from arbitrary free text and must echo expected lane/version/presence identities when confirming delivery.

## Implementation Freedom

The Codex Worker may choose:

- package/file names and internal Go interfaces;
- exact SQLite table/index names;
- opaque ID/token generation implementation;
- whether live presence is held in a dedicated in-memory registry or equivalent process-local structure;
- HTTP response envelope/error type consistent with current API style;
- bounded capability/protocol metadata fields;
- test helper structure and exact config variable names.

The Worker may NOT:

- derive a competing lane router;
- make browser identity a credential/auth system;
- persist liveness in a way that restores `ready` after backend restart without fresh heartbeat;
- allow one active target to serve multiple lanes;
- silently rebind/steal a target;
- accept arbitrary target URL/query/page content as durable identity;
- let browser/frontend state override binding version/readiness authority.

## Verification

Required focused evidence includes:

- first registration, idempotent same-session registration and stale-session takeover;
- simultaneous same-worker different-session conflict;
- target normalization and rejection of unsupported origin/query/fragment authority;
- binding create/read/rebind/disable with monotonic version CAS;
- stale expected-version conflict;
- one-lane/one-target and target-exclusivity conflict tests;
- ready/stale/unavailable/conflict/disabled projections;
- heartbeat TTL with injected clock;
- browser navigation/session restart changing/removing presence token;
- backend restart simulation proving persisted binding is not ready until fresh presence and receives a new token;
- Project/work-unit/lane isolation;
- no credentials/page/response content in persisted binding state;
- backend regression, `go test -race ./...`, exact-Head CI and Web Lead QA.

Because browser target identity is a High-risk consequential-routing boundary, fresh independent review is expected unless the Web Lead explicitly records why residual risk no longer warrants it.

## Dependencies

Product authority dependency: Stage 3 routing is already merged and remains canonical for lane keys.

Parallel/shared contract: #17 consumes the read-only binding snapshot semantics defined here; #18 does not depend on #17 delivery lifecycle and can be implemented in parallel.

Operational dependency: WebLead 3.0 runtime merged through #27.

Owner approval: M6 approved on 2026-07-26.
