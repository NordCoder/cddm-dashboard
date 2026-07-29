# Change #78 — Exact Library session finalization

## Outcome

Complete M11 by turning one durable `surface_ready` creation tab into one verified, registered and atomically bound ChatGPT role session.

## Authorized Base

```text
Branch: change/77
Head: 0fdfabde77d8aa789562708e21181ff0c98be2e0
Dependency: M11-C2 PR #80
```

## Contract

1. The extension receives only the frozen provisioning request created from the active M10 lease and Intent.
2. Every attachment is resolved through the ChatGPT Library UI by NFC-normalized complete filename.
3. Zero, partial, duplicate or multiple matches fail before the send control is touched.
4. Selected attachment chips are verified after each choice and again in frozen profile order.
5. Bootstrap text initializes the role and instructs it to wait for a Workflow Command; it carries no assignment authority.
6. `send_reserved` is persisted before the bootstrap click. Recovery never replays an ambiguous click.
7. The resulting conversation must remain in the configured exact ChatGPT Project scope, or in the global `/c/<id>` scope when no Project is configured.
8. The managed worker uses a restart-stable session ID and must prove one live exact target.
9. Backend finalization atomically revalidates the provisioning claim, current lane lease, Intent, synchronized Issue/PR, exact Head, Project URL, attachment evidence and expected binding version.
10. The same transaction writes or updates the role binding and marks the request `provisioned` with observed URL and binding evidence.
11. Fresh Implementor/QA rotation supersedes prior same-scope session evidence and disables its old binding. A healthy persistent Project Lead may be reused without another bootstrap.

## Durable phases

```text
pending
→ claimed
→ surface_ready
→ provisioned
```

Browser-local bootstrap phases:

```text
not_started
→ send_reserved
→ sent
→ target_observed
→ provisioned
```

Terminal fail-closed states:

```text
safe_failed | uncertain | superseded
```

## Evidence retained

- request, Project, Intent, lease, role, Issue and expected Head identities;
- fixed resource profile and ordered filenames;
- exact attachment evidence;
- managed worker and exact tab identity;
- canonical target plus observed scoped ChatGPT URL;
- binding ID and binding version;
- bounded completion reason and timestamps.

ChatGPT response content is never read or persisted.

## Verification

Repository verification covers:

- exact, partial, missing and duplicate filename fixtures;
- chip cardinality and ordering drift;
- global and Project-scoped URL validation;
- one-shot send and restart recovery;
- `send_reserved` terminal uncertainty;
- target-observed finalize retry without resend;
- persistent Lead reuse;
- fresh QA session rotation;
- stale Head, Project-scope and binding-version rollback;
- atomic old-binding disable/current-binding readiness;
- full backend tests and race detector;
- extension module validation and tests;
- frontend build and packaged-resource validation.

A disposable live smoke against the current ChatGPT Library DOM remains an external integration gate before merge.

## Excluded

- Workflow Command materialization;
- normal assignment delivery;
- worker response scraping;
- Lead merge execution;
- automatic merge;
- public multi-user deployment.
