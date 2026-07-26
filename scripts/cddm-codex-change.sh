#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
core="$repo_root/scripts/cddm-codex-change-core.sh"
shim_dir="$repo_root/.worktrees/host-bin"
mkdir -p "$shim_dir"

[[ -x "$core" ]] || { echo "Missing Host runtime core: $core" >&2; exit 1; }
real_codex="$(command -v codex)"
[[ -n "$real_codex" ]] || { echo "Missing required command: codex" >&2; exit 1; }
export CDDM_REAL_CODEX="$real_codex"
ln -sfn "$repo_root/scripts/cddm-codex-worker-shim.sh" "$shim_dir/codex"

repair_prethread_start_failure() {
  [[ "${1:-}" == "start" && "${2:-}" =~ ^[0-9]+$ ]] || return 0
  local issue="$2" runtime_dir="$repo_root/.worktrees/runtime" state worktree pid events archive_dir
  state="$runtime_dir/issue-$issue.json"
  [[ -f "$state" ]] || return 0
  jq -e '.thread_id == "" and .status == "START_FAILED_NO_THREAD" and .active_mode == "start"' "$state" >/dev/null 2>&1 || return 0

  pid="$(jq -r '.active_pid // ""' "$state")"
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
    echo "Refusing to repair Issue #$issue: prior Codex process is still alive (pid=$pid)." >&2
    return 1
  fi
  events="$(jq -r '.active_events // ""' "$state")"
  if [[ -n "$events" && -s "$events" ]] && jq -e 'select(.type == "thread.started")' "$events" >/dev/null 2>&1; then
    echo "Refusing to repair Issue #$issue: failed-start events contain a thread.started identity." >&2
    return 1
  fi
  worktree="$(jq -r '.worktree // ""' "$state")"
  if [[ -n "$worktree" && -d "$worktree" && -n "$(git -C "$worktree" status --porcelain)" ]]; then
    echo "Refusing to repair Issue #$issue: failed-start worktree is dirty." >&2
    return 1
  fi

  archive_dir="$runtime_dir/archive"
  mkdir -p "$archive_dir"
  mv "$state" "$archive_dir/issue-$issue-prethread-$(date -u +%Y%m%dT%H%M%SZ).json"
  echo "Recovered pre-thread failed start for Issue #$issue; archived stale runtime state." >&2
}

repair_prethread_start_failure "$@"
PATH="$shim_dir:$PATH" exec "$core" "$@"
