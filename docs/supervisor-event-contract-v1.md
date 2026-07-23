# Supervisor Event Contract v1

The Supervisor reads GitHub Issue comments as human-readable operational records. A terminal worker comment may include one machine-readable HTML envelope while keeping all surrounding Markdown free-form.

An operational envelope starts on its own Markdown line outside fenced code and block quotes. Contract examples inside code fences are documentation only and never become workflow evidence.

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "implementor",
  "status": "completed",
  "head": "0123456789abcdef0123456789abcdef01234567"
}
-->
```

## Required fields

Every authoritative envelope contains:

| Field | Meaning |
| --- | --- |
| `v` | Contract version. Version 1 is currently routable. |
| `event` | Event name. Version 1 routes `worker_result`. |
| `role` | Producing role: `lead`, `implementor` or `qa`. |
| `status` | Terminal outcome: `completed`, `no_op` or `blocked`. |

A malformed envelope or a missing required field is a protocol hard error for that comment. The Supervisor does not stop the Project: it retains the comment and requires Lead/manual review while that comment is the latest terminal evidence. A later valid terminal result may safely supersede it; the historical warning remains visible for audit.

Unknown values and additional fields are preserved as warnings or extension data. They do not crash parsing and do not become automatic transitions until the runtime understands them.

## Candidate and QA fields

Candidate-bound results use the full exact PR Head SHA. Head prefixes are not accepted as Candidate identity.

QA results require:

- `head` — exact reviewed Head;
- `verdict` — `approved`, `changes_required` or `inconclusive`.

A new PR Head makes prior Implementor handoff and QA approval evidence stale. Stale evidence remains visible but cannot advance routing. Once QA publishes a new valid verdict for the current Head, an older invalidated approval no longer keeps the work unit in `qa_invalidated` attention.

CI evidence is effective only when its `head_sha` equals the current Candidate Head. Implementor completion or no-op waits for successful exact-Head CI before it advances to QA or Lead; pending, absent or stale CI cannot advance or fail the current Candidate.

## Completed

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "implementor",
  "status": "completed",
  "head": "0123456789abcdef0123456789abcdef01234567"
}
-->
```

An Implementor `completed` result routes to QA when QA is required; otherwise it routes to Lead.

## No-op

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "implementor",
  "status": "no_op",
  "head": "0123456789abcdef0123456789abcdef01234567",
  "checked": ["current Head", "requested scope", "tests"]
}
-->
```

`no_op` is a successful terminal dispatch outcome, not missing work. The human Markdown should state what was checked. Routing treats it like completion and advances to the next role, so the identical Implementor lane is not repeated.

## Blocked

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "qa",
  "status": "blocked",
  "head": "0123456789abcdef0123456789abcdef01234567",
  "verdict": "inconclusive",
  "blocker_kind": "missing_evidence"
}
-->
```

Implementor and QA blockers always route to Lead first. A worker blocker never dispatches directly to Owner.

## QA verdicts

Approved:

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "qa",
  "status": "completed",
  "head": "0123456789abcdef0123456789abcdef01234567",
  "verdict": "approved"
}
-->
```

Changes required:

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "qa",
  "status": "completed",
  "head": "0123456789abcdef0123456789abcdef01234567",
  "verdict": "changes_required"
}
-->
```

Inconclusive:

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "qa",
  "status": "completed",
  "head": "0123456789abcdef0123456789abcdef01234567",
  "verdict": "inconclusive"
}
-->
```

Routing is deterministic: approved → Lead, changes required → Implementor, inconclusive → Lead.

## Lead resolution and escalation

Lead may resolve a blocker and resume a worker role:

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "lead",
  "status": "completed",
  "decision": "continue",
  "resume_role": "qa",
  "resolves": 5061098921
}
-->
```

`decision`, `resume_role` and `resolves` are intentionally forward-compatible. For automatic blocker resolution, `resolves` must correlate to the stable GitHub comment ID of the currently active blocker. It may be a number, numeric string, array, or object containing `comment_id` / `id`. A missing, unknown or non-matching value preserves the blocker and requires Lead/manual attention. Known resume roles are validated before automatic routing; unknown values remain visible and require Lead/manual attention.

Only Lead can escalate to Owner:

```html
<!-- supervisor:event
{
  "v": 1,
  "event": "worker_result",
  "role": "lead",
  "status": "blocked",
  "decision": "owner_required",
  "escalate_to": "owner",
  "resolves": 5061098921
}
-->
```

Owner escalation creates `owner_required` attention and no worker lane dispatch.

## Three parsing levels

1. **Authoritative envelope.** Valid live `supervisor:event` JSON drives exact contract parsing. Fenced or quoted examples are ignored as protocol data.
2. **Legacy heading fallback.** Standard headings such as `Implementor Handoff`, `QA Verdict`, `Blocker`, `Lead Decision`, `Lead Escalation` and `Stage Handoff` are classified with a warning. Automatic routing is allowed only when enough exact data is present.
3. **Unclassified activity.** Other non-empty comments update meaningful activity but do not trigger a state transition.

Stable GitHub comment ID, Project ID and Issue number identify parsed evidence. Duplicate snapshot records with the same comment ID are collapsed deterministically.

## Responsibility boundary

Stage 3 routes only:

```text
Project + Work Unit + Target Role → deterministic lane_key
```

Future responsibilities remain separated:

- **OpenCode** may propose an action and role;
- **Policy Engine** validates whether that proposal is allowed;
- **Lane Router** selects a deterministic role lane;
- **Browser Binding** later maps a lane to a browser profile, tab or chat.

The Stage 3 router does not choose a Chrome profile, tab ID, chat URL or prompt delivery mechanism.
