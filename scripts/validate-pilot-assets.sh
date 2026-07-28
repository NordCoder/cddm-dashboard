#!/usr/bin/env bash
set -euo pipefail

release_profile="resources/cddm-dashboard-resources/v1.0"
runtime_profile="backend/internal/resourcepack/assets/cddm-dashboard-resources/v1.0"

validate_resources() {
  for file in manifest.yaml lead-trigger.md implementor-trigger.md qa-trigger.md worker-result-marker.md worker-result.schema.json; do
    test -s "$release_profile/$file"
    test -s "$runtime_profile/$file"
    cmp --silent "$release_profile/$file" "$runtime_profile/$file"
  done
  python3 -m json.tool "$release_profile/worker-result.schema.json" >/dev/null
}

validate_samples() {
  python3 -m json.tool examples/misak-pilot-project.json >/dev/null
  python3 -m json.tool examples/misak-pilot-execution-profile.json >/dev/null
  bash -n scripts/cddm-pilot-readiness.sh
}

validate_docs() {
  grep -q 'cddm-dashboard-resources/v1.0' docs/worker-loop.md
  grep -q 'cddm-worker-result/v1' docs/worker-loop.md
  grep -q 'manual_fresh_binding' docs/pilot-guide.md
  grep -q 'biakfbpkfdpniphmoafgldedkbnjfibp' docs/installation.md
  grep -q 'http://localhost:1337' docs/configuration.md
  grep -q 'http://localhost:1338' docs/configuration.md
  grep -q 'PILOT READY' docs/pilot-guide.md
}

case "${1:-all}" in
  resources)
    validate_resources
    ;;
  samples)
    validate_samples
    ;;
  docs)
    validate_docs
    ;;
  all)
    validate_resources
    validate_samples
    validate_docs
    ;;
  *)
    echo "usage: $0 [resources|samples|docs|all]" >&2
    exit 2
    ;;
esac
