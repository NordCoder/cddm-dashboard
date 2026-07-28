# Dashboard worker loop

## Protocol identities

```text
Browser Delivery Command
= exact prompt transport to one bound ChatGPT conversation

Workflow Command
= Lead, Implementor, or QA assignment with an expected terminal result

Worker Result
= cddm-worker-result/v1 marker in one GitHub Issue comment

GitHub facts
= authority for PR, Head, CI, QA freshness, mergeability, and merge result
```

`delivered` means only that the extension observed bounded submit acknowledgement. It never means the worker completed the assignment.

## Versioned resources

The repository distributes `resources/cddm-dashboard-resources/v1.0/` with Lead, Implementor, and QA prompts, marker instructions, and JSON Schema. The Go runtime embeds a byte-identical copy under `backend/internal/resourcepack/assets/`; CI prevents drift. The runtime deterministically loads and startup-validates the embedded manifest, version, files and digest. Operational prompts are not loaded from Google Drive.

## Project execution profile

The durable local profile separates delivery authority, chat creation policy and browser destination:

```json
{
  "delivery_mode": "reviewed",
  "qa_session_mode": "manual_fresh_binding",
  "chat_creation_mode": "manual",
  "chatgpt_project_url": "",
  "auto_merge": false
}
```

`chatgpt_project_url` is optional. When set, every Dashboard-created Lead, Implementor or QA conversation for that repository must be bootstrapped and observed inside that exact ChatGPT Project scope before its canonical `/c/<id>` target can be bound. It remains transport configuration and creates no GitHub or Workflow Command authority.

## Command lifecycle

Minimum states:

```text
created → delivery_pending → awaiting_result
                         ↘ failed
awaiting_result → completed | blocked | inconclusive | failed | ambiguous
active old command → superseded
```

One Workflow Command may link to only one Browser Delivery Command. A duplicate link to the same pair is idempotent; a different delivery is rejected.

## Result validation

One GitHub comment may contain at most one marker. Validation distinguishes:

```text
accepted | malformed | unsupported | unbound | wrong_role | stale | ambiguous
```

A marker without `command_id` is legacy/unbound evidence. Conflicting valid terminal results never use “newest wins”; they make the command `ambiguous`.

## External verification

A marker is a claim. Before consequential routing, Dashboard checks synchronized GitHub facts including PR identity, exact Head, exact-Head CI, QA-reviewed Head, mergeability, blockers, merge result, and current main.

Required routing behavior includes:

- `candidate_ready` → verify PR/Head → wait for exact-Head CI;
- QA `blocked_inconclusive` with `blocking_findings=0` and `reason_code=exact_candidate_ci_queued` → keep the same Candidate, create no correction, wait for CI, then request fresh QA on the same Head;
- `changes_required` → Lead correction review;
- approved Head A followed by PR Head B → stale approval;
- conflicting valid markers → `ambiguous` and Lead attention.

## Recovery

Commands, links, results, validation evidence, execution profiles, chat creation mode and ChatGPT Project URL survive restart. Delivery reconciliation preserves `awaiting_result`; it does not replay the DOM send. A terminal marker synchronized after downtime is accepted exactly once, and duplicate synchronization remains idempotent.

## Surfaces

The current Work Unit shows route, active Workflow Command, delivery status, execution status, Worker Result, validation state, Candidate, CI, QA-reviewed Head, warnings, next action, role bindings, ChatGPT Project scope and Pilot Readiness. Historical Prompt Plan screens do not execute current workflow actions.
