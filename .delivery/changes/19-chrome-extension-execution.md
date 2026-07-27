# Change — Execute Confirmed Delivery in Chrome Extension

Milestone: M6 — Browser Prompt Delivery
Risk: High
Issue: #19
Execution state: Shaped — Ready for persistent Change implementation

## Outcome

Implement the Chrome Manifest V3 browser worker that registers its current ChatGPT conversation presence, claims only backend-authorized delivery executions, inserts the exact immutable prompt into that exact bound conversation, sends it at most once, and reports a bounded terminal outcome without reading ChatGPT responses.

## Requirements

- Consume the already-merged #17 delivery command lifecycle and #18 worker/binding protocol without redefining backend authority.
- Generate one stable local `worker_id` per extension installation and a fresh `worker_session_id` per extension runtime.
- Observe only the current/explicitly selected supported ChatGPT conversation; do not scan unrelated tabs or conversations.
- Heartbeat current target presence through #18 and claim through #17 only while the worker/session/target identity is current.
- Execute only the immutable `prompt` returned by a successful `claim-next` response.
- Durably deduplicate `claim_id` before any consequential DOM send.
- A claimed execution is never automatically retried after an ambiguous send/restart; ambiguity reports or converges to `uncertain`.
- Verify target identity immediately before prompt insertion and immediately before the consequential send.
- Do not read, scrape, classify, persist or infer ChatGPT response content or completion.
- Use minimum browser permissions and a user-configured backend origin; do not use broad unrelated host access.

## Existing Backend Contract

The extension must integrate with the current merged HTTP surface rather than invent a second protocol:

- `POST /api/browser/workers` — register worker/session/current target;
- `POST /api/browser/workers/{worker_id}/heartbeat` — refresh #18 presence;
- `POST /api/browser/deliveries/claim-next` — atomically claim one pending #17 execution;
- `POST /api/browser/deliveries/{command_id}/complete` — complete that exact claim.

`claim-next` request fields are exactly the current #17 claim identity:

- `worker_id`;
- `worker_session_id`;
- `claim_request_id`.

A successful execution payload contains the immutable command snapshot, `claim_id`, and exact canonical `prompt`. HTTP `204` means no executable command is currently available.

## HARD HOW

### 1. Extension architecture and trust boundary

Use Chrome Manifest V3 with a background service worker plus the minimum ChatGPT page integration needed to identify the current supported conversation and operate its compose/send surface.

The browser extension is an executor, not a planner/router/policy engine. It must never:

- create or modify Prompt Plans;
- derive lane routing;
- choose a different target when the commanded target is unavailable;
- substitute prompt text;
- infer backend delivery eligibility from local browser state;
- read ChatGPT responses.

All consequential authority arrives only through a successful #17 claim.

### 2. Stable worker identity and runtime session

On first extension installation, generate an opaque random `worker_id` and persist it in `chrome.storage.local`. Reuse it across browser restarts and extension service-worker suspension.

Generate a fresh opaque `worker_session_id` for each extension runtime start. It must not survive a full extension/browser runtime restart.

Registration and heartbeat use the same identity until that runtime ends. A backend conflict for the worker/session is surfaced as unavailable/conflict state; the extension does not silently manufacture a second worker identity to bypass it.

### 3. Backend origin configuration

The extension has one explicit operator-configured backend base URL for the local/private CDDM deployment.

- Normalize it to an HTTP(S) origin/base URL without credentials, query or fragment.
- Do not persist backend passwords/tokens because M6 introduces no new public auth model.
- Request only the host permission needed for that configured backend origin, preferably via `optional_host_permissions` / runtime permission rather than `<all_urls>`.
- ChatGPT page access is limited to the explicitly supported `https://chatgpt.com/*` surface required for target observation and compose/send.

A missing/invalid/unpermitted backend origin leaves the worker disabled and cannot produce claims.

### 4. ChatGPT target normalization

The only M6 target kind is `chatgpt_conversation`.

The extension must share #18 semantic normalization:

- origin must normalize to `https://chatgpt.com`;
- query and fragment are ignored/rejected as authority;
- a bindable target must identify a stable supported conversation path/opaque conversation key;
- generic pages/new-chat surfaces without a stable conversation identity are not bindable/executable;
- navigation to another conversation changes the observed target immediately.

The extension may inspect the URL and the minimum DOM required to operate the composer. It must not inspect response-message text to derive target identity or execution outcome.

### 5. Presence and heartbeat

When a supported current conversation is observable, register/heartbeat #18 with:

- stable worker ID;
- current worker-session ID;
- bounded protocol/capability metadata;
- normalized current target.

When no supported target is current, heartbeat `null`/unavailable target state rather than retaining the last conversation.

Heartbeat cadence must keep a legitimately active target inside the current backend liveness TTL while the extension is active. Service-worker suspension must not be interpreted locally as durable presence; after runtime restart, a fresh session must re-register and re-prove target presence.

No page text, prompt content, response content, cookies, ChatGPT auth data or arbitrary URL query data is included in heartbeat payloads.

### 6. Claim polling semantics

Claim polling is serial per worker session: at most one `claim-next` request/claimed execution may be processed by the extension at a time.

For each logical poll attempt:

- generate one opaque `claim_request_id`;
- reuse that same request ID only for transport retry of that same unresolved HTTP request;
- after a definitive `204`/success/failure response, a later poll uses a fresh request ID.

The extension must not poll/claim while disabled, unconfigured, in worker-session conflict, or lacking current supported target presence.

A successful execution must be rejected locally without DOM send if any immutable execution identity is inconsistent with the live extension state, including worker ID, worker-session ID, target kind/ref or claim identity. Such an inconsistency is consequentially ambiguous and must never trigger target substitution.

### 7. Durable browser-side claim ledger

Backend at-most-once claim semantics are necessary but not sufficient for an external DOM side effect. Before touching the ChatGPT composer, persist a browser-local ledger entry keyed by `claim_id` in `chrome.storage.local`.

Minimum local states/meaning:

- `reserved` — this claim has been accepted by the extension and must never be executed by another local attempt;
- `sent` — the extension positively performed its compose/send action;
- `failed_pre_send` — the extension positively proved no send occurred;
- `uncertain` — execution may have occurred and automatic retry is forbidden;
- acknowledgement metadata sufficient to retry only the backend completion acknowledgement, never the ChatGPT send.

If a `claim_id` already exists in any ledger state, the extension must not perform the ChatGPT send again.

If extension/browser restart finds a `reserved` claim without proof that the consequential action did not occur, it becomes/acts as `uncertain`; it is never automatically re-executed.

Bound ledger growth with safe pruning of old terminal records, but never prune a live/unacknowledged claim in a way that permits replay.

### 8. DOM execution boundary

Immediately before insertion and again immediately before send:

1. resolve the active/current operating tab owned by this extension flow;
2. normalize its target;
3. require exact equality with the command `target_kind` + `target_ref`;
4. require the expected compose surface to be present and usable.

Do not search other tabs for a matching conversation and do not open/navigate to a different conversation automatically.

Insert exactly the immutable `Execution.prompt` returned by #17. Do not prepend/append/edit whitespace or derive prompt text from the dashboard/page.

The DOM adapter may support a small ordered selector strategy for the current ChatGPT composer/send controls, but it must fail closed if selectors are ambiguous, missing, disabled, or no longer correspond to the supported compose surface.

### 9. Outcome classification

After claim, completion is one of:

- `delivered` only when the extension positively performed the send action for the exact reserved claim;
- `failed` only for a bounded pre-send failure where the extension can positively assert that no consequential send occurred;
- `uncertain` whenever the extension cannot prove whether the send occurred.

Reason/evidence values are bounded machine-oriented diagnostics. They must not contain page text, ChatGPT responses, cookies/tokens or the full prompt.

Examples of safe pre-send failure categories include target mismatch, unsupported target, compose unavailable and permission unavailable. A failure after a send click/submit was attempted is not automatically `failed`; if the side effect cannot be proven absent, report `uncertain`.

### 10. Completion acknowledgement

Completion uses the exact `command_id` + `claim_id` from the claimed execution.

An identical completion may be retried to the backend because #17 acknowledgement is idempotent. A retry of backend acknowledgement must never repeat the ChatGPT DOM send.

Conflicting completion response from backend is surfaced as a diagnostic and does not rewrite local execution history or resend.

### 11. Restart and offline behavior

- Browser/extension runtime restart creates a new worker-session ID and requires fresh #18 presence.
- Existing durable `worker_id` remains stable.
- Existing local claim ledger remains durable.
- No startup routine may automatically replay a prior claimed/reserved/sent command.
- Backend offline/network failure before claim has no consequential effect.
- Network failure after claim but before/after acknowledgement follows ledger + uncertain semantics, never send retry semantics.

### 12. Permissions and privacy

Manifest review must demonstrate that permissions are limited to:

- extension local storage;
- minimum tab/scripting capabilities needed for the current target;
- `https://chatgpt.com/*`;
- the explicit configured backend origin.

Do not request browsing history, cookies, webRequest interception, clipboard, downloads, broad `<all_urls>` access, or unrelated site permissions unless an unavoidable need is demonstrated and separately approved.

No ChatGPT response content is stored locally or sent to backend logs/events.

## Implementation Freedom

The Worker may choose:

- extension source directory/build tooling consistent with the repository;
- TypeScript module boundaries;
- exact MV3 service-worker/content-script messaging structure;
- bounded selector adapter implementation;
- local ledger storage shape and safe pruning policy;
- heartbeat/poll interval implementation within backend TTL semantics;
- extension options/popup presentation for backend configuration and diagnostics;
- test framework and browser fixture structure.

The Worker may NOT change #17/#18 lifecycle/identity semantics, add automatic resend after claim, scan alternate conversations, read response content, or broaden browser authority into routing/policy.

## Verification

Required focused evidence includes:

- stable installation worker ID and fresh runtime session ID;
- worker registration/heartbeat with current-target and no-target transitions;
- target normalization and navigation-away behavior;
- serial claim polling and same-request transport retry idempotency;
- exact `worker_id`/session/target validation before execution;
- durable claim ledger written before DOM side effect;
- duplicate `claim_id` never sends twice, including simulated extension restart;
- exact prompt insertion without mutation;
- target re-check immediately before send;
- clear pre-send `failed` vs ambiguous `uncertain` classification;
- completion acknowledgement retry without DOM resend;
- backend offline/restart and browser restart safety;
- static manifest/permission review proving no response/cookie/broad-site access;
- practical browser integration verification against a supported ChatGPT conversation;
- extension unit/integration tests plus existing backend/frontend regressions;
- exact-Head CI and fresh Web Lead QA.

Because this Change performs a consequential external send, fresh independent safety review is expected unless the Web Lead explicitly records why residual risk no longer warrants it.

## Dependencies

#17 and #18 are merged and production-wired. Their canonical contracts are `.delivery/changes/17-browser-delivery-command.md` and `.delivery/changes/18-browser-lane-binding.md`.

Owner approval: M6 approved on 2026-07-26.
