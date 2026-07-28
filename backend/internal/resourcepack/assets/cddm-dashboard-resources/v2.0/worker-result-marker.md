# CDDM Worker Result Marker v2

Publish one human-readable GitHub Issue comment followed by exactly one live HTML marker:

```html
<!-- cddm-dashboard:result {"version":2,"role":"implementor","result":"candidate_ready","command_id":"cmd-...","pr":123,"head":"0123456789abcdef0123456789abcdef01234567"} -->
```

The marker must not be inside a Markdown quote, indented code block or fenced code block.

## Common fields

- `version`: exactly `2`;
- `role`: `lead`, `implementor` or `qa`;
- `result`: allowed for that role;
- `command_id`: exact Dashboard command identity.

Unknown fields are invalid. All Candidate SHAs are full lowercase 40-character values.

## Implementor

- `candidate_ready`: `pr`, `head`;
- `continue`: no additional identity;
- `blocked`: `blocker_type`, `reason_code`;
- `no_op`: `reason_code`.

## QA

Every QA result includes `reviewed_head` and `blocking_findings`.

- `approved`: findings = 0;
- `changes_required`: findings > 0 and `cycle_escalation`;
- `blocked`: findings = 0 plus `blocker_type`, `reason_code`;
- `inconclusive`: findings = 0 plus `blocker_type`, `reason_code`.

## Lead

- `actions_ready`: non-empty ordered `actions`; optional `wave`;
- `merged`: `repository`, `issue`, `pr`, `approved_head`, `merge_commit`;
- `hold`: `reason_code`;
- `owner_required`: `reason_code`.

Each action follows `action-vocabulary.md`. Action IDs are unique inside the result. When `wave` is present, Wave Issue membership and dispatch actions must agree.

A marker is a claim until Dashboard correlates `command_id` and verifies GitHub facts. M9 persists accepted action batches; action materialization begins in M10.
