#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
core="$repo_root/scripts/cddm-codex-change-core.sh"
shim_dir="$repo_root/.worktrees/host-bin"
mkdir -p "$shim_dir"

[[ -x "$core" ]] || { echo "Missing Host runtime core: $core" >&2; exit 1; }
real_codex="$(command -v codex)"
[[ -n "$real_codex" ]] || { echo "Missing required command: codex" >&2; exit 1; }
command -v setsid >/dev/null 2>&1 || { echo "Missing required command: setsid" >&2; exit 1; }
command -v pkill >/dev/null 2>&1 || { echo "Missing required command: pkill" >&2; exit 1; }
export CDDM_REAL_CODEX="$real_codex"
ln -sfn "$repo_root/scripts/cddm-codex-worker-shim.sh" "$shim_dir/codex"

pid_is_active_core_turn() {
  local pid="$1" mode="$2" issue="$3" cmdline
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  [[ -r "/proc/$pid/cmdline" ]] || return 1
  cmdline="$(tr '\0' ' ' <"/proc/$pid/cmdline" 2>/dev/null)" || return 1
  [[ "$cmdline" == *"$core"* ]] || return 1
  [[ "$cmdline" == *" $mode $issue "* || "$cmdline" == *" $mode $issue" ]] || return 1
}

repair_prethread_start_failure() {
  [[ "${1:-}" == "start" && "${2:-}" =~ ^[0-9]+$ ]] || return 0
  local issue="$2" runtime_dir="$repo_root/.worktrees/runtime" state worktree pid events archive_dir
  state="$runtime_dir/issue-$issue.json"
  [[ -f "$state" ]] || return 0
  jq -e '.thread_id == "" and .status == "START_FAILED_NO_THREAD" and .active_mode == "start"' "$state" >/dev/null 2>&1 || return 0

  pid="$(jq -r '.active_pid // ""' "$state")"
  if pid_is_active_core_turn "$pid" start "$issue"; then
    echo "Refusing to repair Issue #$issue: prior Codex host turn is still alive (pid=$pid)." >&2
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

align_clean_prethread_orphan() {
  [[ "${1:-}" == "start" && "${2:-}" =~ ^[0-9]+$ ]] || return 0
  local issue="$2" state branch worktree actual_branch actual_head expected_head
  state="$repo_root/.worktrees/runtime/issue-$issue.json"
  [[ ! -f "$state" ]] || return 0

  branch="change/$issue"
  worktree="$repo_root/.worktrees/issue-$issue"
  [[ -d "$worktree" ]] || return 0

  actual_branch="$(git -C "$worktree" branch --show-current)"
  [[ "$actual_branch" == "$branch" ]] || {
    echo "Refusing to realign Issue #$issue: orphan worktree is on '$actual_branch', expected '$branch'." >&2
    return 1
  }
  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || {
    echo "Refusing to realign Issue #$issue: orphan worktree is dirty." >&2
    return 1
  }

  git fetch --prune origin --quiet
  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    expected_head="$(git rev-parse "origin/$branch")"
  else
    expected_head="$(git rev-parse origin/main)"
  fi
  actual_head="$(git -C "$worktree" rev-parse HEAD)"
  [[ "$actual_head" != "$expected_head" ]] || return 0

  git merge-base --is-ancestor "$actual_head" "$expected_head" || {
    echo "Refusing to realign Issue #$issue: orphan Head has unique/divergent history (actual=$actual_head expected=$expected_head)." >&2
    return 1
  }
  git -C "$worktree" reset --hard "$expected_head" >/dev/null
  echo "Realigned clean pre-thread orphan worktree for Issue #$issue to $expected_head." >&2
}

recover_dead_interrupted_turn() {
  [[ "${1:-}" =~ ^(start|resume|rotate|status)$ && "${2:-}" =~ ^[0-9]+$ ]] || return 0
  local issue="$2" runtime_dir="$repo_root/.worktrees/runtime" state pid active_mode result exit_status events stored_thread found_thread pid_file archive_dir tmp
  state="$runtime_dir/issue-$issue.json"
  [[ -f "$state" ]] || return 0
  jq -e '(.active_mode // "") != "" or (.active_pid // null) != null' "$state" >/dev/null 2>&1 || return 0

  pid="$(jq -r '.active_pid // ""' "$state")"
  active_mode="$(jq -r '.active_mode // ""' "$state")"
  if pid_is_active_core_turn "$pid" "$active_mode" "$issue"; then
    return 0
  fi
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
    echo "Ignoring reused/stale active PID for Issue #$issue: pid=$pid is not the recorded $active_mode host turn." >&2
  fi

  result="$(jq -r '.active_result // ""' "$state")"
  exit_status="$(jq -r '.active_exit_status // ""' "$state")"
  # Completed/dead turns with durable evidence belong to the core recovery path.
  [[ -z "$result" || ! -s "$result" ]] || return 0
  [[ -z "$exit_status" || ! -s "$exit_status" ]] || return 0

  events="$(jq -r '.active_events // ""' "$state")"
  stored_thread="$(jq -r '.thread_id // ""' "$state")"
  found_thread=""
  if [[ -n "$events" && -s "$events" ]]; then
    found_thread="$(jq -r 'select(.type=="thread.started") | .thread_id // .thread.id // empty' "$events" 2>/dev/null | head -n1)"
  fi
  if [[ -n "$found_thread" && -n "$stored_thread" && "$found_thread" != "$stored_thread" ]]; then
    echo "Refusing interrupted-turn recovery for Issue #$issue: event thread does not match persisted thread." >&2
    return 1
  fi

  archive_dir="$runtime_dir/archive"
  mkdir -p "$archive_dir"
  cp "$state" "$archive_dir/issue-$issue-interrupted-$(date -u +%Y%m%dT%H%M%SZ).json"
  pid_file="$(jq -r '.active_pid_file // ""' "$state")"
  [[ -z "$pid_file" ]] || rm -f "$pid_file"

  tmp="$state.tmp"
  jq --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
    .active_pid=null
    | .active_pid_file=null
    | .active_mode=null
    | .active_events=null
    | .active_result=null
    | .active_v2_log=null
    | .active_exit_status=null
    | .active_previous_thread=null
    | .active_rotation_reason=null
    | .active_model=null
    | .active_reasoning=null
    | .updated_at=$updated_at
  ' "$state" >"$tmp"
  mv "$tmp" "$state"
  echo "Recovered dead interrupted turn for Issue #$issue; preserved thread and worktree, archived stale active state." >&2
}

run_core_isolated() {
  local session_pid rc signal=""

  terminate_session() {
    signal="$1"
    trap - INT TERM
    if [[ -n "${session_pid:-}" ]] && kill -0 "$session_pid" 2>/dev/null; then
      # The core runs as a dedicated session leader. Kill the whole session so
      # Codex/code-mode descendants cannot survive Ctrl+C and retain the lock.
      pkill -TERM -s "$session_pid" 2>/dev/null || true
      kill -TERM "$session_pid" 2>/dev/null || true
    fi
  }

  PATH="$shim_dir:$PATH" setsid "$core" "$@" &
  session_pid=$!
  trap 'terminate_session INT' INT
  trap 'terminate_session TERM' TERM
  set +e
  wait "$session_pid"
  rc=$?
  set -e
  trap - INT TERM

  case "$signal" in
    INT) return 130 ;;
    TERM) return 143 ;;
    *) return "$rc" ;;
  esac
}

repair_prethread_start_failure "$@"
align_clean_prethread_orphan "$@"
recover_dead_interrupted_turn "$@"
run_core_isolated "$@"
