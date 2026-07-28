# CDDM Worker Result Marker v1

Every Dashboard-issued worker command terminates by publishing one GitHub Issue comment containing human-readable Markdown and at most one live marker:

```html
<!-- cddm-dashboard:result
{
  "version": 1,
  "role": "implementor",
  "result": "candidate_ready",
  "command_id": "cmd-opaque",
  "pr": 150,
  "head": "241401d9f5c1fb2004eeb19ec612323f74b57199"
}
-->
```

Rules:

- The marker must start on its own non-code Markdown line.
- The payload is one valid UTF-8 JSON object conforming to `worker-result.schema.json`.
- One comment contains at most one marker.
- `version` is `1`.
- `command_id` is mandatory for Dashboard-issued work.
- A marker without a known command is preserved as unbound evidence and cannot complete the active command.
- GitHub comment ID is the durable ingestion and deduplication identity.
- Marker fields are claims. Dashboard independently verifies consequential GitHub facts such as PR, exact Head, CI, mergeability, merge result and Issue state.
- Browser prompt delivery is not execution completion.
