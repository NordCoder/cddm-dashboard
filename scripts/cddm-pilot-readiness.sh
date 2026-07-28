#!/usr/bin/env bash
set -euo pipefail

api_origin="${1:-http://localhost:1337}"
project_id="${2:-}"
issue_number="${3:-}"

if [[ -z "$project_id" || -z "$issue_number" ]]; then
  echo "usage: $0 [api-origin] <project-id> <issue-number>" >&2
  exit 2
fi

payload="$(curl --fail --silent --show-error \
  "${api_origin%/}/api/projects/${project_id}/work-units/${issue_number}/pilot-readiness")"

python3 - "$payload" <<'PY'
import json
import sys

value = json.loads(sys.argv[1])
print(f"Pilot Readiness: {value.get('status', 'unknown')}")
for check in value.get("checks", []):
    state = "READY" if check.get("ready") else "BLOCKED"
    detail = f" — {check['detail']}" if check.get("detail") else ""
    print(f"[{state}] {check.get('code')}: {check.get('status')}{detail}")
for warning in value.get("protocol_warnings", []):
    print(f"[WARNING] {warning.get('code')}: {warning.get('message')}")
if value.get("ready") is not True or value.get("status") != "pilot_ready":
    raise SystemExit(1)
print("PILOT READY")
PY
