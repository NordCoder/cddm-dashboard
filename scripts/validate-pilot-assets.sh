#!/usr/bin/env bash
set -euo pipefail

profile="resources/cddm-dashboard-resources/v1.0"
for file in manifest.yaml lead-trigger.md implementor-trigger.md qa-trigger.md worker-result-marker.md worker-result.schema.json; do
  test -s "$profile/$file"
done

python3 -m json.tool "$profile/worker-result.schema.json" >/dev/null
python3 -m json.tool examples/misak-pilot-project.json >/dev/null
python3 -m json.tool examples/misak-pilot-execution-profile.json >/dev/null

bash -n scripts/cddm-pilot-readiness.sh

grep -q 'cddm-dashboard-resources/v1.0' docs/worker-loop.md
grep -q 'cddm-worker-result/v1' docs/worker-loop.md
grep -q 'manual_fresh_binding' docs/pilot-guide.md
grep -q 'biakfbpkfdpniphmoafgldedkbnjfibp' docs/installation.md
grep -q 'http://localhost:1337' docs/configuration.md
grep -q 'http://localhost:1338' docs/configuration.md
grep -q 'PILOT READY' docs/pilot-guide.md
