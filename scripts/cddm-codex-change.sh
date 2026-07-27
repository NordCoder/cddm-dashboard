#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
bin="${CDDM_CHANGE_BIN:-$repo_root/.worktrees/bin/cddm-change}"
build="$repo_root/scripts/build-cddm-change.sh"

if [[ ! -x "$bin" ]]; then
  echo "CDDM Go runtime is not built: $bin" >&2
  echo "Build it with:" >&2
  echo "  ./scripts/build-cddm-change.sh" >&2
  exit 78
fi

built_revision="$($bin __build-revision 2>/dev/null || true)"
current_revision="$(git -C "$repo_root" rev-parse HEAD)"
if [[ -n "$built_revision" && "$built_revision" != "dev" && "$built_revision" != "$current_revision" ]]; then
  echo "CDDM Go runtime is stale." >&2
  echo "  built:   $built_revision" >&2
  echo "  current: $current_revision" >&2
  echo "Rebuild it with:" >&2
  echo "  ./scripts/build-cddm-change.sh" >&2
  exit 78
fi

exec env CDDM_REPO_ROOT="$repo_root" CDDM_CHANGE_BIN="$bin" "$bin" "$@"
