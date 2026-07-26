#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "Usage: scripts/cddm-publish-branch.sh" >&2
  exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

branch="$(git branch --show-current)"
if [[ ! "$branch" =~ ^change/[0-9]+$ ]]; then
  echo "Refusing to publish non-Change branch '$branch'. Expected change/<issue>." >&2
  exit 1
fi

origin_url="$(git remote get-url origin)"
case "$origin_url" in
  "https://github.com/NordCoder/cddm-dashboard"|\
  "https://github.com/NordCoder/cddm-dashboard.git"|\
  "git@github.com:NordCoder/cddm-dashboard.git"|\
  "ssh://git@github.com/NordCoder/cddm-dashboard.git")
    ;;
  *)
    echo "Refusing to publish to unexpected origin: $origin_url" >&2
    exit 1
    ;;
esac

# Fixed destination: the current bounded Change branch only. No caller-controlled refspecs.
git push --set-upstream origin "HEAD:refs/heads/$branch"
