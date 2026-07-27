#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
core="$repo_root/scripts/cddm-codex-change-core.sh"
observer="$repo_root/scripts/cddm-codex-observe.py"
worker_shim="$repo_root/scripts/cddm-codex-worker-shim.sh"
host_tool_shim="$repo_root/scripts/cddm-host-tool-shim.sh"
shim_dir="$repo_root/.worktrees/host-bin"
mkdir -p "$shim_dir"

usage() {
  cat <<'TXT'
Usage:
  scripts/cddm-codex-change.sh start     <issue> [model] [reasoning]
  scripts/cddm-codex-change.sh resume    <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh rotate    <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh recover   <issue>
  scripts/cddm-codex-change.sh stop      <issue>
  scripts/cddm-codex-change.sh reconcile <issue>
  scripts/cddm-codex-change.sh status    <issue> [--json]
  scripts/cddm-codex-change.sh watch     <issue> [--stall-seconds N]
  scripts/cddm-codex-change.sh logs      <issue> [--raw|--v2]
  scripts/cddm-codex-change.sh turns     <issue> [--limit N]
TXT
}

[[ $# -ge 2 ]] || { usage >&2; exit 2; }
command_name="$1"
issue="$2"
[[ "$issue" =~ ^[0-9]+$ ]] || { echo "Issue must be numeric." >&2; exit 2; }

# Observer commands are deliberately dispatched before Codex/GitHub/runtime-lock
# setup. They read durable local artifacts only and remain usable while a Host
# turn owns the Issue lock.
case "$command_name" in
  status|watch|logs|turns)
    [[ -f "$observer" ]] || { echo "Missing runtime observer: $observer" >&2; exit 1; }
    command -v python3 >/dev/null 2>&1 || { echo "Missing required command: python3" >&2; exit 1; }
    exec python3 "$observer" "$command_name" --repo "$repo_root" --issue "$issue" "${@:3}"
    ;;
esac

case "$command_name" in
  start|resume|rotate|recover|stop|reconcile) ;;
  *) usage >&2; exit 2 ;;
esac

[[ -x "$core" ]] || { echo "Missing Host runtime core: $core" >&2; exit 1; }
command -v setsid >/dev/null 2>&1 || { echo "Missing required command: setsid" >&2; exit 1; }
command -v pkill >/dev/null 2>&1 || { echo "Missing required command: pkill" >&2; exit 1; }
command -v flock >/dev/null 2>&1 || { echo "Missing required command: flock" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "Missing required command: jq" >&2; exit 1; }
command -v ps >/dev/null 2>&1 || { echo "Missing required command: ps" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "Missing required command: python3" >&2; exit 1; }

# Resolve real tools before prepending shim_dir to PATH. Host V2 shims are
# transparent outside CDDM_HOST_V2_UI=1; the Worker shim unsets that flag.
export CDDM_REPO_ROOT="$repo_root"
for tool in go gofmt npm docker tee; do
  real="$(command -v "$tool" 2>/dev/null || true)"
  case "$tool" in
    go) export CDDM_REAL_GO="$real" ;;
    gofmt) export CDDM_REAL_GOFMT="$real" ;;
    npm) export CDDM_REAL_NPM="$real" ;;
    docker) export CDDM_REAL_DOCKER="$real" ;;
    tee) export CDDM_REAL_TEE="$real" ;;
  esac
  if [[ -n "$real" && -x "$host_tool_shim" ]]; then
    ln -sfn "$host_tool_shim" "$shim_dir/$tool"
  fi
done

setup_codex_shim() {
  local allow_blocker="${1:-0}" real_codex
  real_codex="$(command -v codex 2>/dev/null || true)"
  if [[ -n "$real_codex" ]]; then
    export CDDM_REAL_CODEX="$real_codex"
  elif [[ "$allow_blocker" == "1" ]]; then
    # Recovery-only core startup still executes `codex login status` in the
    # legacy core. The Worker shim short-circuits that probe while
    # CDDM_BLOCK_CODEX=1 and refuses every execution path.
    export CDDM_REAL_CODEX=/bin/false
  else
    echo "Missing required command: codex" >&2
    return 1
  fi
  ln -sfn "$worker_shim" "$shim_dir/codex"
}

pid_is_active_core_turn() {
  local pid="$1" mode="$2" target_issue="$3" cmdline
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  [[ -r "/proc/$pid/cmdline" ]] || return 1
  cmdline="$(tr '\0' ' ' <"/proc/$pid/cmdline" 2>/dev/null)" || return 1
  [[ "$cmdline" == *"$core"* ]] || return 1
  [[ "$cmdline" == *" $mode $target_issue "* || "$cmdline" == *" $mode $target_issue" ]] || return 1
}

session_is_owned_core_turn() {
  local pid="$1" mode="$2" target_issue="$3" sid leader_cmdline
  pid_is_active_core_turn "$pid" "$mode" "$target_issue" || return 1
  sid="$(ps -o sid= -p "$pid" 2>/dev/null | tr -d '[:space:]')"
  [[ "$sid" =~ ^[0-9]+$ ]] || return 1
  [[ -r "/proc/$sid/cmdline" ]] || return 1
  leader_cmdline="$(tr '\0' ' ' <"/proc/$sid/cmdline" 2>/dev/null)" || return 1
  [[ "$leader_cmdline" == *"$core"* ]] || return 1
  [[ "$leader_cmdline" == *" $mode $target_issue "* || "$leader_cmdline" == *" $mode $target_issue" ]] || return 1
  printf '%s' "$sid"
}

repair_prethread_start_failure() {
  [[ "${1:-}" == "start" && "${2:-}" =~ ^[0-9]+$ ]] || return 0
  local target_issue="$2" runtime_dir="$repo_root/.worktrees/runtime" state worktree pid events archive_dir
  state="$runtime_dir/issue-$target_issue.json"
  [[ -f "$state" ]] || return 0
  jq -e '.thread_id == "" and .status == "START_FAILED_NO_THREAD" and .active_mode == "start"' "$state" >/dev/null 2>&1 || return 0

  pid="$(jq -r '.active_pid // ""' "$state")"
  if pid_is_active_core_turn "$pid" start "$target_issue"; then
    echo "Refusing to repair Issue #$target_issue: prior Codex host turn is still alive (pid=$pid)." >&2
    return 1
  fi
  events="$(jq -r '.active_events // ""' "$state")"
  if [[ -n "$events" && -s "$events" ]] && jq -e 'select(.type == "thread.started")' "$events" >/dev/null 2>&1; then
    echo "Refusing to repair Issue #$target_issue: failed-start events contain a thread.started identity." >&2
    return 1
  fi
  worktree="$(jq -r '.worktree // ""' "$state")"
  if [[ -n "$worktree" && -d "$worktree" && -n "$(git -C "$worktree" status --porcelain)" ]]; then
    echo "Refusing to repair Issue #$target_issue: failed-start worktree is dirty." >&2
    return 1
  fi

  archive_dir="$runtime_dir/archive"
  mkdir -p "$archive_dir"
  mv "$state" "$archive_dir/issue-$target_issue-prethread-$(date -u +%Y%m%dT%H%M%SZ).json"
  echo "Recovered pre-thread failed start for Issue #$target_issue; archived stale runtime state." >&2
}

align_clean_prethread_orphan() {
  [[ "${1:-}" == "start" && "${2:-}" =~ ^[0-9]+$ ]] || return 0
  local target_issue="$2" state branch worktree actual_branch actual_head expected_head
  state="$repo_root/.worktrees/runtime/issue-$target_issue.json"
  [[ ! -f "$state" ]] || return 0

  branch="change/$target_issue"
  worktree="$repo_root/.worktrees/issue-$target_issue"
  [[ -d "$worktree" ]] || return 0

  actual_branch="$(git -C "$worktree" branch --show-current)"
  [[ "$actual_branch" == "$branch" ]] || {
    echo "Refusing to realign Issue #$target_issue: orphan worktree is on '$actual_branch', expected '$branch'." >&2
    return 1
  }
  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || {
    echo "Refusing to realign Issue #$target_issue: orphan worktree is dirty." >&2
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
    echo "Refusing to realign Issue #$target_issue: orphan Head has unique/divergent history (actual=$actual_head expected=$expected_head)." >&2
    return 1
  }
  git -C "$worktree" reset --hard "$expected_head" >/dev/null
  echo "Realigned clean pre-thread orphan worktree for Issue #$target_issue to $expected_head." >&2
}

recover_dead_interrupted_turn() {
  [[ "${1:-}" =~ ^(start|resume|rotate)$ && "${2:-}" =~ ^[0-9]+$ ]] || return 0
  local target_issue="$2" runtime_dir="$repo_root/.worktrees/runtime" state pid active_mode result exit_status events stored_thread found_thread pid_file archive_dir tmp
  state="$runtime_dir/issue-$target_issue.json"
  [[ -f "$state" ]] || return 0
  jq -e '(.active_mode // "") != "" or (.active_pid // null) != null' "$state" >/dev/null 2>&1 || return 0

  pid="$(jq -r '.active_pid // ""' "$state")"
  active_mode="$(jq -r '.active_mode // ""' "$state")"
  if pid_is_active_core_turn "$pid" "$active_mode" "$target_issue"; then
    return 0
  fi
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
    echo "Ignoring reused/stale active PID for Issue #$target_issue: pid=$pid is not the recorded $active_mode host turn." >&2
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
    echo "Refusing interrupted-turn recovery for Issue #$target_issue: event thread does not match persisted thread." >&2
    return 1
  fi

  archive_dir="$runtime_dir/archive"
  mkdir -p "$archive_dir"
  cp "$state" "$archive_dir/issue-$target_issue-interrupted-$(date -u +%Y%m%dT%H%M%SZ).json"
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
  echo "Recovered dead interrupted turn for Issue #$target_issue; preserved thread and worktree, archived stale active state." >&2
}

reconcile_change() {
  [[ "${1:-}" == "reconcile" && "${2:-}" =~ ^[0-9]+$ ]] || { echo "Usage: $0 reconcile <issue>" >&2; return 2; }
  local target_issue="$2" runtime_dir state branch worktree lock_file local_head remote_head candidate_head candidate_parent new_base patch_dir patch_file apply_rc tmp
  runtime_dir="$repo_root/.worktrees/runtime"
  state="$runtime_dir/issue-$target_issue.json"
  branch="change/$target_issue"
  worktree="$repo_root/.worktrees/issue-$target_issue"
  lock_file="$runtime_dir/issue-$target_issue.lock"

  [[ -f "$state" ]] || { echo "No runtime state for Issue #$target_issue." >&2; return 1; }
  [[ -d "$worktree" ]] || { echo "Persistent worktree is missing: $worktree" >&2; return 1; }
  exec 8>"$lock_file"
  flock -n 8 || { echo "Another host operation is active for Issue #$target_issue." >&2; return 1; }

  [[ "$(git branch --show-current)" == "main" ]] || { echo "Run reconcile from the controlling main checkout." >&2; return 1; }
  [[ -z "$(git status --porcelain)" ]] || { echo "Controlling main must be clean." >&2; return 1; }
  jq -e '(.active_pid // null) == null and (.active_mode // null) == null' "$state" >/dev/null || {
    echo "Issue #$target_issue has an active or unreconciled turn; recover it before base reconciliation." >&2
    return 1
  }
  [[ "$(git -C "$worktree" branch --show-current)" == "$branch" ]] || { echo "Unexpected branch in persistent worktree." >&2; return 1; }
  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || { echo "Issue #$target_issue worktree must be clean before base reconciliation." >&2; return 1; }

  git fetch --prune origin --quiet
  git merge --ff-only origin/main --quiet
  new_base="$(git rev-parse origin/main)"
  local_head="$(git -C "$worktree" rev-parse HEAD)"
  git show-ref --verify --quiet "refs/remotes/origin/$branch" || { echo "Remote Change branch is missing: origin/$branch" >&2; return 1; }
  remote_head="$(git rev-parse "origin/$branch")"
  candidate_head="$(jq -r '.candidate_head // ""' "$state")"
  candidate_parent="$(jq -r '.candidate_parent // ""' "$state")"

  [[ -n "$candidate_head" && -n "$candidate_parent" ]] || { echo "Issue #$target_issue has no published Candidate identity to reconcile." >&2; return 1; }
  [[ "$local_head" == "$candidate_head" && "$remote_head" == "$candidate_head" ]] || {
    echo "Candidate identity mismatch before reconcile: state=$candidate_head local=$local_head remote=$remote_head" >&2
    return 1
  }
  [[ "$candidate_head" != "$new_base" ]] || { echo "Issue #$target_issue Candidate is already based at current main."; return 0; }
  git -C "$worktree" cat-file -e "$candidate_parent^{commit}" 2>/dev/null || { echo "Candidate parent object is missing: $candidate_parent" >&2; return 1; }

  patch_dir="$runtime_dir/reconcile"
  mkdir -p "$patch_dir"
  patch_file="$patch_dir/issue-$target_issue-${candidate_head:0:12}-onto-${new_base:0:12}.patch"
  git -C "$worktree" diff --binary "$candidate_parent" "$candidate_head" >"$patch_file"
  [[ -s "$patch_file" ]] || { echo "Candidate patch is empty; refusing reconcile." >&2; return 1; }

  git -C "$worktree" reset --hard "$new_base" >/dev/null
  set +e
  git -C "$worktree" apply --3way --whitespace=nowarn "$patch_file"
  apply_rc=$?
  set -e
  if [[ $apply_rc -ne 0 && -z "$(git -C "$worktree" status --porcelain)" ]]; then
    git -C "$worktree" reset --hard "$candidate_head" >/dev/null
    echo "Candidate patch could not be applied and produced no reconcilable worktree changes; restored old Candidate." >&2
    return 1
  fi
  git -C "$worktree" reset --mixed >/dev/null

  if ! git push --force-with-lease="refs/heads/$branch:$remote_head" origin "HEAD:refs/heads/$branch" >/dev/null; then
    git -C "$worktree" reset --hard "$candidate_head" >/dev/null
    echo "Failed to move remote Change branch to the new integration base; restored old local Candidate." >&2
    return 1
  fi

  tmp="$state.tmp"
  jq --arg from_head "$candidate_head" --arg base "$new_base" --arg patch "$patch_file" --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
    .status="RECONCILING"
    | .reconcile_from_head=$from_head
    | .reconcile_base=$base
    | .reconcile_patch=$patch
    | .candidate_head=null
    | .candidate_parent=null
    | .candidate_remote_before=null
    | .candidate_result=null
    | .updated_at=$updated_at
  ' "$state" >"$tmp"
  mv "$tmp" "$state"

  echo "Prepared Issue #$target_issue reconciliation onto $new_base (previous Candidate $candidate_head, apply_rc=$apply_rc)."
  echo "Worktree changes are preserved for the existing persistent Change thread."
  git -C "$worktree" status --short
}

run_core_isolated() {
  local session_pid rc signal="" mode="${1:-}" target_issue="${2:-}" block_codex="${CDDM_BLOCK_CODEX:-0}"

  terminate_session() {
    signal="$1"
    trap - INT TERM
    if [[ -n "${session_pid:-}" ]] && kill -0 "$session_pid" 2>/dev/null; then
      pkill -TERM -s "$session_pid" 2>/dev/null || true
      kill -TERM "$session_pid" 2>/dev/null || true
    fi
  }

  CDDM_REPO_ROOT="$repo_root" CDDM_RUNTIME_MODE="$mode" CDDM_RUNTIME_ISSUE="$target_issue" \
    CDDM_HOST_V2_UI=1 CDDM_BLOCK_CODEX="$block_codex" PATH="$shim_dir:$PATH" \
    setsid "$core" "$@" &
  session_pid=$!
  trap 'terminate_session INT' INT
  trap 'terminate_session TERM' TERM
  set +e
  wait "$session_pid"
  rc=$?
  set -e
  trap - INT TERM

  if [[ -f "$observer" && "$target_issue" =~ ^[0-9]+$ ]]; then
    python3 "$observer" record-recovery --repo "$repo_root" --issue "$target_issue" >/dev/null 2>&1 || true
  fi

  case "$signal" in
    INT) return 130 ;;
    TERM) return 143 ;;
    *) return "$rc" ;;
  esac
}

recover_change() {
  local target_issue="$1" state before_active rc after_active status last_rc
  state="$repo_root/.worktrees/runtime/issue-$target_issue.json"
  [[ -f "$state" ]] || { echo "No runtime state for Issue #$target_issue."; return 0; }
  before_active="$(jq -r '((.active_mode // "") != "" or (.active_pid // null) != null)' "$state")"

  setup_codex_shim 1
  set +e
  CDDM_BLOCK_CODEX=1 run_core_isolated resume "$target_issue" /dev/null
  rc=$?
  set -e

  python3 "$observer" record-recovery --repo "$repo_root" --issue "$target_issue" >/dev/null 2>&1 || true
  after_active="$(jq -r '((.active_mode // "") != "" or (.active_pid // null) != null)' "$state")"
  status="$(jq -r '.status // "UNKNOWN"' "$state")"
  last_rc="$(jq -r '.last_result_rc // empty' "$state")"

  if [[ "$after_active" == "false" ]]; then
    if [[ "$before_active" == "true" ]]; then
      echo "Recovered Issue #$target_issue active turn: status=$status${last_rc:+, prior_rc=$last_rc}."
    else
      echo "Issue #$target_issue has no active turn to recover."
    fi
    python3 "$observer" status --repo "$repo_root" --issue "$target_issue"
    return 0
  fi

  echo "Recovery remains fail-closed for Issue #$target_issue: status=$status rc=$rc." >&2
  python3 "$observer" status --repo "$repo_root" --issue "$target_issue" >&2 || true
  return "$rc"
}

stop_change() {
  local target_issue="$1" state pid mode sid direct_child waited
  state="$repo_root/.worktrees/runtime/issue-$target_issue.json"
  [[ -f "$state" ]] || { echo "No runtime state for Issue #$target_issue."; return 0; }
  pid="$(jq -r '.active_pid // ""' "$state")"
  mode="$(jq -r '.active_mode // ""' "$state")"

  if [[ -z "$mode" || ! "$pid" =~ ^[0-9]+$ ]]; then
    echo "Issue #$target_issue has no active Host turn."
    recover_change "$target_issue"
    return
  fi

  if ! sid="$(session_is_owned_core_turn "$pid" "$mode" "$target_issue")"; then
    if kill -0 "$pid" 2>/dev/null; then
      echo "Refusing stop: persisted pid=$pid is alive but is not the owned $mode Host turn for Issue #$target_issue." >&2
      return 1
    fi
    echo "Recorded Issue #$target_issue turn is already dead; reconciling durable state."
    recover_change "$target_issue"
    return
  fi

  echo "Stopping Issue #$target_issue $mode turn (pid=$pid session=$sid)…"
  # First stop the Codex/proxy child and let the Host subshell/core record a
  # durable exit status. This avoids recreating the old Ctrl+C unknown-state
  # failure mode when normal shutdown is responsive.
  direct_child="$(pgrep -P "$pid" 2>/dev/null | head -n1 || true)"
  if [[ "$direct_child" =~ ^[0-9]+$ ]]; then
    kill -TERM "$direct_child" 2>/dev/null || true
  else
    kill -TERM "$pid" 2>/dev/null || true
  fi

  waited=0
  while kill -0 "$pid" 2>/dev/null && (( waited < 50 )); do
    sleep 0.1
    ((waited+=1))
  done

  if kill -0 "$pid" 2>/dev/null; then
    echo "Host turn did not settle after descendant TERM; terminating owned session $sid." >&2
    pkill -TERM -s "$sid" 2>/dev/null || true
    sleep 1
  fi
  if kill -0 "$pid" 2>/dev/null; then
    echo "Owned session $sid still alive; escalating to KILL." >&2
    pkill -KILL -s "$sid" 2>/dev/null || true
    sleep 0.2
  fi

  recover_change "$target_issue"
}

case "$command_name" in
  reconcile)
    reconcile_change "$@"
    ;;
  recover)
    recover_change "$issue"
    ;;
  stop)
    stop_change "$issue"
    ;;
  start|resume|rotate)
    setup_codex_shim 0
    repair_prethread_start_failure "$@"
    align_clean_prethread_orphan "$@"
    recover_dead_interrupted_turn "$@"
    run_core_isolated "$@"
    ;;
esac
