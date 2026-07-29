# Change #77 — Headless Managed Session Provisioner

## Outcome

Consume durable M11 provisioning requests directly from the Chrome extension service worker and create one exact managed ChatGPT tab/worker identity per request without requiring an open Dashboard page.

## Authorized base

```text
M11-C1 Candidate: d7b33fd41360eb3dd15182f3f6f8e6a1d18e9796
Branch: change/77
```

## Runtime model

The primary manual browser worker remains separate from managed role workers.

Each managed record persists:

- provisioning request and claim identity;
- worker ID and independent runtime session identity;
- exact Chrome tab ID;
- Project, Intent, Issue, role, lane and expected Head;
- configured ChatGPT Project surface;
- optional final conversation target;
- durable local completion-retry state.

Managed records live in extension local storage and are revalidated after every service-worker restart.

## Pre-bootstrap surface boundary

ChatGPT does not reliably expose a `/c/<id>` conversation URL until the first message is submitted. M11-C2 deliberately sends no message.

Therefore:

```text
surface_ready = exact Chrome tab + managed worker identity + verified ChatGPT creation scope
```

The conversation target may remain empty. M11-C3 is responsible for exact Library resolution, bootstrap submission, observing `/c/<id>`, final worker presence and role binding.

This avoids sending a placeholder message without the required Library resources.

## Headless operation

- The extension service worker polls provisioning through its existing timer/alarm scheduler.
- No Dashboard page or external page message is required.
- Tabs are created inactive in global ChatGPT or the configured exact ChatGPT Project surface.
- A local managed record is persisted before backend `surface_ready` acknowledgement.
- If acknowledgement fails, the same exact tab is retried after restart; a duplicate tab is not created.
- Managed workers heartbeat independently from the primary worker.
- Normal assignment delivery remains disabled for a managed record until its durable status becomes `provisioned` in M11-C3.

## Failure semantics

- Failure before a tab exists is `safe_failed`.
- A created tab that cannot be proven to reach the requested ChatGPT creation scope is `uncertain` and never replayed automatically.
- A closed or drifted pre-bootstrap managed tab is completed as `safe_failed` and removed locally.
- An exact final target is never replaced by another active ChatGPT tab.

## Safety boundary

M11-C2 does not:

- select or verify Library attachments;
- submit bootstrap text;
- bind a role lane;
- create Workflow Commands;
- deliver normal assignments;
- read ChatGPT responses;
- merge code.

## Verification

Required evidence:

- global and Project-scoped inactive tab creation;
- no DOM message during surface creation;
- no-Dashboard alarm polling;
- independent managed registration with `target=null`;
- restart reuse without duplicate tabs;
- backend completion retry using the same exact tab;
- safe/uncertain classification;
- closed-tab retirement and no retarget;
- no normal delivery claims before final provisioning;
- extension tests and exact-Head CI;
- fresh independent QA.
