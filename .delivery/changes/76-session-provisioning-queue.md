# Change #76 — Durable Session Provisioning Queue

## Outcome

Convert an exact active M10 lane lease into a durable, typed ChatGPT session-provisioning request. The queue is consumed by the Chrome extension in later M11 Changes; this Change does not create a browser tab.

## Authorized base

```text
M10-C3 Candidate: 6188ea4da5c66ad48c574ba607e9d641a1f0a751
Branch: change/76
```

## Durable model

A provisioning request stores only bounded bootstrap/session facts:

- Project, Intent, active lane lease and lane;
- Issue, role and exact Candidate Head when applicable;
- frozen v2 attachment-profile identity and ordered filenames;
- bounded role-bootstrap text that instructs the worker to wait;
- persistent-Lead or fresh-per-Intent session policy;
- configured ChatGPT Project surface;
- expected browser-binding version;
- claim, exact worker/tab/target and completion evidence.

It does not store a generated assignment prompt or ChatGPT response content.

## Lifecycle

```text
pending
→ claimed
→ surface_ready
→ provisioned
```

Failure terminals:

```text
safe_failed | uncertain | superseded
```

`surface_ready` means that an exact conversation surface and worker identity have already been created, but no bootstrap send is yet authorized. It survives extension service-worker restart and prevents a second tab from being created for the same request.

Only an expired `claimed` request may return to `pending`. `surface_ready` and terminal outcomes are never automatically replayed.

## Authority and safety

- Enqueue requires the exact current M10 lease owner and token.
- Role, Issue, lane and Head are read from the durable Intent and cannot be supplied by the caller.
- Attachment filenames are read from `cddm-dashboard-resources/v2.0`.
- Project autonomy must be continuous and enabled before extension claim.
- Completion requires the exact provisioning claim owner and token.
- `provisioned` requires attachment evidence byte-order equivalent to the frozen profile.
- Direct merge and Workflow Command creation remain outside M11-C1.

## APIs

- `GET|PUT /api/projects/{project}/autonomy-profile`
- `GET /api/projects/{project}/session-provisioning`
- `POST /api/projects/{project}/session-provisioning/enqueue`
- `POST /api/browser/provisioning/claim-next`
- `POST /api/browser/provisioning/{request}/complete`

The browser endpoints are local/private runtime transport and remain protected by the existing mutation-origin guard.

## Verification

Required evidence:

- migration and restart compatibility;
- exact lease/token enqueue and idempotency;
- paused Project no-claim behavior;
- claim recovery and terminal uncertain behavior;
- exact ordered attachment evidence;
- HTTP profile/queue/claim contracts;
- backend tests and race detector;
- fresh exact-Head QA.
