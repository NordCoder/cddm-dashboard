#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  if ! command -v gh >/dev/null 2>&1; then
    echo "GitHub CLI is required when GITHUB_TOKEN is not set." >&2
    echo "Install gh, run: gh auth login --git-protocol ssh" >&2
    exit 1
  fi
  if ! gh auth status >/dev/null 2>&1; then
    echo "GitHub CLI is not authenticated. Run: gh auth login --git-protocol ssh" >&2
    exit 1
  fi
  GITHUB_TOKEN="$(gh auth token)"
  export GITHUB_TOKEN
fi

exec docker compose up --build "$@"
