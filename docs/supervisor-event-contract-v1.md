# Supervisor Event Contract v1

The Supervisor reads GitHub Issue comments as human-readable operational records. A terminal worker comment may include one machine-readable HTML envelope while keeping all surrounding Markdown free-form.

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

A malformed envelope or a missing required field is a protocol hard error for that comment. The Supervisor does not stop the Project: it retains the comment, emits protocol attention and requires Lead/manual review.

Unknown values and additional fields are preserved as warnings or extension data. They do not crash parsing and do not become automatic transitions until the runtime understands them.

## Candidate and QA fields

Candidate-bound results use the full exact PR Head SHA. Head prefixes are not accepted as Candidate identity.

QA results require:

- `head` — exact reviewed Head;
- `verdict` — `approved`, `changes_required` or `inconclusive`.

A new PR Head makes prior Implementor handoff and QA approval evidence stale. Stale evidence remains visible but cannot advance routing.

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

`decision`, `resume_role` and `resolves` are intentionally forward-compatible. Known resume roles are validated before automatic routing; unknown values remain visible and require Lead/manual attention.

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

1. **Authoritative envelope.** Valid `supervisor:event` JSON drives exact contract parsing.
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
