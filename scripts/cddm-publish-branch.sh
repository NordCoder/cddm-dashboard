#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: scripts/cddm-publish-branch.sh <worktree>" >&2
  exit 2
fi

control_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
[[ -d "$1" ]] || { echo "Worktree does not exist: $1" >&2; exit 1; }
worktree="$(cd "$1" && pwd -P)"

case "$worktree" in
  "$control_root/.worktrees/issue-"*) ;;
  *)
    echo "Refusing to publish unexpected worktree: $worktree" >&2
    exit 1
    ;;
esac

branch="$(git -C "$worktree" rev-parse --abbrev-ref HEAD)"
if [[ ! "$branch" =~ ^change/[0-9]+$ ]]; then
  echo "Refusing to publish non-Change branch '$branch'." >&2
  exit 1
fi

origin_url="$(git -C "$control_root" remote get-url origin)"
case "$origin_url" in
  "https://github.com/NordCoder/cddm-dashboard"|\
  "https://github.com/NordCoder/cddm-dashboard.git"|\
  "git@github.com:NordCoder/cddm-dashboard.git"|\
  "ssh://git@github.com/NordCoder/cddm-dashboard.git") ;;
  *)
    echo "Refusing to publish through unexpected canonical origin: $origin_url" >&2
    exit 1
    ;;
esac

# Use the validated canonical fetch URL directly; configured pushurl is intentionally ignored.
git -C "$worktree" push "$origin_url" "HEAD:refs/heads/$branch"
