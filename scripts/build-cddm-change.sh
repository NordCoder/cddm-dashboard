#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
out="${CDDM_CHANGE_BIN:-$repo_root/.worktrees/bin/cddm-change}"
revision="$(git -C "$repo_root" rev-parse HEAD)"

mkdir -p "$(dirname "$out")"
(
  cd "$repo_root/backend"
  go build -trimpath -ldflags "-X main.buildRevision=$revision" -o "$out" ./cmd/cddm-change
)
chmod +x "$out"
echo "Built CDDM Go runtime: $out"
echo "Revision: $revision"
