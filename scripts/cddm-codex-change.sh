#!/usr/bin/env bash
set -euo pipefail

repo_slug="NordCoder/cddm-dashboard"
permission_profile="cddm-worker"

usage() {
  cat <<'TXT'
Usage:
  scripts/cddm-codex-change.sh start  <issue> [model] [reasoning]
  scripts/cddm-codex-change.sh resume <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh rotate <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh status <issue>

Defaults:
  model     gpt-5.6-terra
  reasoning medium
TXT
}

[[ $# -ge 2 ]] || { usage >&2; exit 2; }
command_name="$1"
issue="$2"
shift 2
[[ "$issue" =~ ^[0-9]+$ ]] || { echo "Issue must be numeric." >&2; exit 2; }
case "$command_name" in start|resume|rotate|status) ;; *) usage >&2; exit 2 ;; esac

for command in git gh codex jq python3 flock; do
  command -v "$command" >/dev/null 2>&1 || { echo "Missing required command: $command" >&2; exit 1; }
done

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
[[ "$(git branch --show-current)" == "main" ]] || { echo "Run from the controlling main checkout." >&2; exit 1; }
[[ -z "$(git status --porcelain)" ]] || { echo "Controlling main must be clean." >&2; exit 1; }

origin_url="$(git remote get-url origin)"
case "$origin_url" in
  "https://github.com/NordCoder/cddm-dashboard"|"https://github.com/NordCoder/cddm-dashboard.git"|"git@github.com:NordCoder/cddm-dashboard.git"|"ssh://git@github.com/NordCoder/cddm-dashboard.git") ;;
  *) echo "Unexpected canonical origin: $origin_url" >&2; exit 1 ;;
esac

gh auth status >/dev/null 2>&1 || { echo "GitHub CLI is not authenticated. Run: gh auth login" >&2; exit 1; }
codex login status >/dev/null 2>&1 || { echo "Codex CLI is not authenticated. Run: codex login" >&2; exit 1; }
git config user.name >/dev/null || { echo "Git user.name is not configured." >&2; exit 1; }
git config user.email >/dev/null || { echo "Git user.email is not configured." >&2; exit 1; }

git fetch --prune origin --quiet
git merge --ff-only origin/main --quiet
[[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] || { echo "Local main differs from origin/main." >&2; exit 1; }

runtime_dir="$repo_root/.worktrees/runtime"
results_dir="$repo_root/.worktrees/results"
mkdir -p "$runtime_dir" "$results_dir"
state_file="$runtime_dir/issue-$issue.json"
lock_file="$runtime_dir/issue-$issue.lock"
exec 9>"$lock_file"
flock -n 9 || { echo "Another host operation is active for Issue #$issue." >&2; exit 1; }

branch="change/$issue"
worktree="$repo_root/.worktrees/issue-$issue"
worker_home="$worktree/.cddm-worker-home"
schema="$repo_root/.codex/schemas/change-turn-result.json"
start_template="$repo_root/.codex/prompts/change-start.md"
resume_template="$repo_root/.codex/prompts/change-resume.md"
rotate_template="$repo_root/.codex/prompts/change-rotate.md"
[[ -f "$schema" ]] || { echo "Missing result schema: $schema" >&2; exit 1; }

resolve_contract() {
  local matches=()
  shopt -s nullglob
  matches=("$repo_root"/.delivery/changes/"$issue"-*.md)
  shopt -u nullglob
  [[ ${#matches[@]} -le 1 ]] || { echo "Multiple Change Contracts found for Issue #$issue." >&2; return 1; }
  if [[ ${#matches[@]} -eq 1 ]]; then printf '%s' "${matches[0]#"$repo_root"/}"; else printf '%s' none; fi
}

contract="$(resolve_contract)"
recovered_turn_dispatched=0

issue_context() {
  gh issue view "$issue" --repo "$repo_slug" --json title,body \
    --jq '"ISSUE TITLE: " + .title + "\n\nISSUE BODY:\n" + (.body // "")'
}

find_pr_for_branch() {
  gh pr list --repo "$repo_slug" --head "$branch" --state open --json number --jq '.[0].number // empty'
}

ensure_pr() {
  local found title
  found="$(find_pr_for_branch)" || return 1
  if [[ -z "$found" ]]; then
    title="$(gh issue view "$issue" --repo "$repo_slug" --json title --jq '.title')" || return 1
    gh pr create --repo "$repo_slug" --draft --base main --head "$branch" --title "$title" \
      --body "Closes #$issue

Canonical Change Contract: \`$contract\`

CDDM WebLead 3.0: Web Lead owns WHAT + HARD HOW + QA; implementation uses one persistent Codex Change session unless explicitly rotated." >/dev/null || return 1
    found="$(find_pr_for_branch)" || return 1
  fi
  [[ -n "$found" ]] || { echo "Unable to resolve/create PR for $branch." >&2; return 1; }
  printf '%s' "$found"
}

verify_pr_head() {
  local pr="$1" expected="$2" actual
  actual="$(gh pr view "$pr" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
  [[ "$actual" == "$expected" ]] || { echo "PR Head mismatch: expected=$expected actual=$actual" >&2; return 1; }
}

remote_branch_head() {
  git ls-remote "$origin_url" "refs/heads/$branch" | awk 'NR == 1 { print $1 }'
}

state_init() {
  local model="$1" reasoning="$2" status="$3" tmp="$state_file.tmp"
  jq -n --argjson version 3 --arg issue "$issue" --arg branch "$branch" --arg worktree "$worktree" \
    --arg model "$model" --arg reasoning "$reasoning" --arg contract "$contract" --arg status "$status" \
    --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{version:$version,issue:($issue|tonumber),branch:$branch,worktree:$worktree,thread_id:"",model:$model,reasoning:$reasoning,contract:$contract,status:$status,thread_turn_count:0,total_turn_count:0,thread_generation:1,thread_history:[],candidate_head:null,candidate_parent:null,candidate_remote_before:null,candidate_result:null,pr:null,active_pid:null,active_pid_file:null,active_mode:null,active_events:null,active_result:null,active_v2_log:null,active_previous_thread:null,active_rotation_reason:null,active_model:null,active_reasoning:null,last_result:null,last_result_rc:null,last_counted_result:null,updated_at:$updated_at}' >"$tmp"
  mv "$tmp" "$state_file"
}

state_patch_status() {
  local status="$1" candidate_head="${2:-}" pr="${3:-}" tmp="$state_file.tmp"
  jq --arg status "$status" --arg candidate_head "$candidate_head" --arg pr "$pr" \
    --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.status=$status | .updated_at=$updated_at
     | .candidate_head=(if $candidate_head=="" then .candidate_head else $candidate_head end)
     | .pr=(if $pr=="" then .pr else ($pr|tonumber) end)' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_set_prepared_candidate() {
  local result="$1" head="$2" parent="$3" remote_before="$4" tmp="$state_file.tmp"
  jq --arg result "$result" --arg head "$head" --arg parent "$parent" --arg remote_before "$remote_before" --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.status="COMMIT_PREPARED" | .candidate_result=$result | .candidate_head=$head | .candidate_parent=$parent | .candidate_remote_before=$remote_before | .updated_at=$updated_at' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_set_thread() {
  local thread_id="$1" status="$2" tmp="$state_file.tmp"
  jq --arg thread_id "$thread_id" --arg status "$status" --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.thread_id=$thread_id | .status=$status | .updated_at=$updated_at' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_set_execution() {
  local model="$1" reasoning="$2" tmp="$state_file.tmp"
  jq --arg model "$model" --arg reasoning "$reasoning" --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.model=$model | .reasoning=$reasoning | .updated_at=$updated_at' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_set_active_intent() {
  local mode="$1" events="$2" result="$3" v2_log="$4" pid_file="$5" previous_thread="$6" rotation_reason="$7" model="$8" reasoning="$9" tmp="$state_file.tmp"
  jq --arg mode "$mode" --arg events "$events" --arg result "$result" --arg v2_log "$v2_log" --arg pid_file "$pid_file" --arg previous_thread "$previous_thread" \
    --arg rotation_reason "$rotation_reason" --arg model "$model" --arg reasoning "$reasoning" \
    --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.active_pid=null | .active_pid_file=$pid_file | .active_mode=$mode | .active_events=$events
     | .active_result=$result | .active_v2_log=$v2_log
     | .active_previous_thread=$previous_thread | .active_rotation_reason=$rotation_reason
     | .active_model=$model | .active_reasoning=$reasoning | .updated_at=$updated_at' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_attach_active_pid() {
  local pid="$1" tmp="$state_file.tmp"
  jq --argjson pid "$pid" --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.active_pid=$pid | .updated_at=$updated_at' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_clear_active() {
  local tmp="$state_file.tmp"
  jq --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.active_pid=null | .active_pid_file=null | .active_mode=null | .active_events=null
     | .active_result=null | .active_v2_log=null
     | .active_previous_thread=null | .active_rotation_reason=null | .active_model=null | .active_reasoning=null
     | .updated_at=$updated_at' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_set_result_disposition() {
  local result="$1" rc="$2" tmp="$state_file.tmp"
  jq --arg result "$result" --argjson rc "$rc" --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.last_result=$result | .last_result_rc=$rc | .updated_at=$updated_at' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_record_turn_for_result() {
  local result="$1" tmp="$state_file.tmp"
  jq --arg result "$result" --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    'if (.last_counted_result // "") == $result then .
     else .thread_turn_count=((.thread_turn_count // 0)+1)
       | .total_turn_count=((.total_turn_count // 0)+1)
       | .last_counted_result=$result
       | .updated_at=$updated_at
     end' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

state_rotate_thread() {
  local new_thread="$1" model="$2" reasoning="$3" reason="$4" tmp="$state_file.tmp"
  jq --arg new_thread "$new_thread" --arg model "$model" --arg reasoning "$reasoning" --arg reason "$reason" \
    --arg rotated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.thread_history=((.thread_history // []) + [{thread_id:.thread_id,model:.model,reasoning:.reasoning,turn_count:(.thread_turn_count // 0),rotated_at:$rotated_at,reason:$reason}])
     | .thread_id=$new_thread | .model=$model | .reasoning=$reasoning | .thread_turn_count=0
     | .thread_generation=((.thread_generation // 1)+1) | .status="ROTATED" | .updated_at=$rotated_at' "$state_file" >"$tmp"
  mv "$tmp" "$state_file"
}

ensure_worker_home() {
  mkdir -p "$worker_home/.config/gh" "$worker_home/.cache" "$worker_home/.local/share"
  local exclude_file
  exclude_file="$(git -C "$worktree" rev-parse --git-path info/exclude)"
  grep -Fxq '.cddm-worker-home/' "$exclude_file" 2>/dev/null || printf '%s\n' '.cddm-worker-home/' >>"$exclude_file"
}

expected_initial_head() {
  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then git rev-parse "origin/$branch"; else git rev-parse origin/main; fi
}

validate_existing_worktree_for_adoption() {
  [[ -d "$worktree" ]] || return 1
  local actual_root expected_head actual_head actual_branch
  actual_root="$(cd "$(git -C "$worktree" rev-parse --show-toplevel)" && pwd -P)"
  [[ "$actual_root" == "$(cd "$worktree" && pwd -P)" ]] || { echo "Unexpected worktree root." >&2; return 1; }
  actual_branch="$(git -C "$worktree" branch --show-current)"
  [[ "$actual_branch" == "$branch" ]] || { echo "Orphan worktree is on '$actual_branch', expected '$branch'." >&2; return 1; }
  ensure_worker_home
  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || { echo "Orphan worktree is dirty; Web Lead reconciliation required." >&2; return 1; }
  expected_head="$(expected_initial_head)"
  actual_head="$(git -C "$worktree" rev-parse HEAD)"
  [[ "$actual_head" == "$expected_head" ]] || { echo "Orphan worktree Head is not canonical: expected=$expected_head actual=$actual_head" >&2; return 1; }
}

create_or_attach_worktree() {
  if [[ -d "$worktree" ]]; then
    validate_existing_worktree_for_adoption
    return
  fi
  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    local remote_head local_head
    remote_head="$(git rev-parse "origin/$branch")"
    if git show-ref --verify --quiet "refs/heads/$branch"; then
      local_head="$(git rev-parse "$branch")"
      [[ "$local_head" == "$remote_head" ]] || { echo "Local $branch diverges from origin/$branch." >&2; return 1; }
    else
      git branch --track "$branch" "origin/$branch" >/dev/null
    fi
    git worktree add "$worktree" "$branch" >/dev/null
  elif git show-ref --verify --quiet "refs/heads/$branch"; then
    [[ "$(git rev-parse "$branch")" == "$(git rev-parse origin/main)" ]] || { echo "Unpublished local $branch is not the canonical initial Head." >&2; return 1; }
    git worktree add "$worktree" "$branch" >/dev/null
  else
    git worktree add -b "$branch" "$worktree" origin/main >/dev/null
  fi
  ensure_worker_home
  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || { echo "Change worktree is unexpectedly dirty." >&2; return 1; }
}

thread_from_events() {
  local events="$1"
  [[ -f "$events" ]] || return 0
  jq -r 'select(.type=="thread.started") | .thread_id // .thread.id // empty' "$events" 2>/dev/null | head -n1
}

run_strict() (
  set -e
  "$@"
)

reconcile_completed_turn_thread() {
  local mode="$1" events="$2" previous="$3" model="$4" reasoning="$5" rotation_reason="$6"
  local found stored
  found="$(thread_from_events "$events")"
  [[ -n "$found" ]] || {
    case "$mode" in
      start) state_patch_status START_FAILED_NO_THREAD ;;
      rotate) state_patch_status ROTATE_FAILED_NO_THREAD ;;
      *) state_patch_status THREAD_MISMATCH ;;
    esac
    echo "Completed $mode turn has no valid thread.started identity." >&2
    return 1
  }
  stored="$(jq -r '.thread_id // ""' "$state_file")"
  case "$mode" in
    start)
      if [[ -z "$stored" ]]; then
        state_set_thread "$found" RUNNING
      elif [[ "$stored" != "$found" ]]; then
        state_patch_status THREAD_MISMATCH
        echo "Completed start turn thread identity does not match persisted state." >&2
        return 2
      fi
      ;;
    resume)
      if [[ -z "$previous" || "$found" != "$previous" || "$stored" != "$previous" ]]; then
        state_patch_status THREAD_MISMATCH
        echo "Completed resume turn is not bound to the expected thread." >&2
        return 2
      fi
      ;;
    rotate)
      if [[ -z "$previous" || "$found" == "$previous" ]]; then
        state_patch_status ROTATE_FAILED_NO_THREAD
        echo "Completed rotate turn did not establish a fresh thread." >&2
        return 2
      fi
      if [[ "$stored" == "$previous" ]]; then
        state_rotate_thread "$found" "$model" "$reasoning" "$rotation_reason"
      elif [[ "$stored" != "$found" ]]; then
        state_patch_status THREAD_MISMATCH
        echo "Completed rotate turn thread identity does not match persisted state." >&2
        return 2
      fi
      ;;
    *)
      state_patch_status THREAD_MISMATCH
      echo "Unknown active turn mode: $mode" >&2
      return 2
      ;;
  esac
}

recover_active_turn_state() {
  [[ -f "$state_file" ]] || return 0
  local mode events result v2_log previous rotation_reason active_model active_reasoning found stored pid pid_file dispatch_rc consumed thread_rc
  mode="$(jq -r '.active_mode // ""' "$state_file")"
  events="$(jq -r '.active_events // ""' "$state_file")"
  result="$(jq -r '.active_result // ""' "$state_file")"
  v2_log="$(jq -r '.active_v2_log // ""' "$state_file")"
  [[ -n "$mode" || -n "$events" || -n "$result" ]] || return 0
  previous="$(jq -r '.active_previous_thread // ""' "$state_file")"
  rotation_reason="$(jq -r '.active_rotation_reason // ""' "$state_file")"
  active_model="$(jq -r '.active_model // .model // "gpt-5.6-terra"' "$state_file")"
  active_reasoning="$(jq -r '.active_reasoning // .reasoning // "medium"' "$state_file")"
  found="$(thread_from_events "$events")"
  stored="$(jq -r '.thread_id // ""' "$state_file")"

  if [[ -n "$found" ]]; then
    case "$mode" in
      start)
        if [[ -z "$stored" ]]; then state_set_thread "$found" RUNNING; elif [[ "$stored" != "$found" ]]; then state_patch_status THREAD_MISMATCH; return 2; fi
        ;;
      resume)
        [[ -z "$previous" || "$found" == "$previous" ]] || { state_patch_status THREAD_MISMATCH; return 2; }
        ;;
      rotate)
        if [[ "$stored" == "$previous" ]]; then
          [[ "$found" != "$previous" ]] || { state_patch_status ROTATE_FAILED_NO_THREAD; return 2; }
          state_rotate_thread "$found" "$active_model" "$active_reasoning" "$rotation_reason"
        elif [[ "$stored" != "$found" ]]; then
          state_patch_status THREAD_MISMATCH
          return 2
        fi
        ;;
    esac
  fi

  pid="$(jq -r '.active_pid // ""' "$state_file")"
  pid_file="$(jq -r '.active_pid_file // ""' "$state_file")"
  if [[ ! "$pid" =~ ^[0-9]+$ && -n "$pid_file" ]]; then
    for _ in {1..20}; do
      if [[ -s "$pid_file" ]]; then pid="$(cat "$pid_file")"; break; fi
      sleep 0.05
    done
  fi
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
    echo "A prior Codex turn is still active for Issue #$issue (pid=$pid)." >&2
    return 3
  fi

  # A completed Codex turn is accepted only with the exact result path and the
  # thread.started identity that belongs to this recorded turn.
  if [[ -n "$result" && -s "$result" ]]; then
    set +e
    run_strict reconcile_completed_turn_thread "$mode" "$events" "$previous" "$active_model" "$active_reasoning" "$rotation_reason"
    thread_rc=$?
    set -e
    [[ $thread_rc -eq 0 ]] || return 4

    state_record_turn_for_result "$result"
    if ! validate_result "$result"; then
      state_patch_status INVALID_RESULT
      state_set_result_disposition "$result" 13
      [[ -z "$pid_file" ]] || rm -f "$pid_file"
      state_clear_active
      echo "Recovered completed turn has an invalid structured result: $result" >&2
      return 13
    fi

    set +e
    run_strict dispatch_result_file "$result" "${v2_log:-$results_dir/issue-$issue-recovered-v2.log}"
    dispatch_rc=$?
    set -e
    consumed="$(jq -r '.last_result // ""' "$state_file")"
    if [[ "$consumed" == "$result" ]]; then
      recovered_turn_dispatched=1
      [[ -z "$pid_file" ]] || rm -f "$pid_file"
      state_clear_active
    else
      echo "Recovered result was not durably consumed: $result" >&2
      return "${dispatch_rc:-4}"
    fi
    return "$dispatch_rc"
  fi

  [[ -z "$pid_file" ]] || rm -f "$pid_file"
  state_clear_active
  return 0
}

ensure_start_runtime() {
  local model="$1" reasoning="$2"
  if [[ ! -f "$state_file" ]]; then
    if [[ -d "$worktree" ]]; then
      validate_existing_worktree_for_adoption
      state_init "$model" "$reasoning" "INITIALIZING"
    else
      state_init "$model" "$reasoning" "WORKTREE_INITIALIZING"
      create_or_attach_worktree
      state_patch_status "INITIALIZING"
    fi
  else
    recover_active_turn_state
    [[ "$recovered_turn_dispatched" == 0 ]] || return 0
    local thread
    thread="$(jq -r '.thread_id // ""' "$state_file")"
    [[ -z "$thread" ]] || { echo "Persistent thread already exists for Issue #$issue; use resume/status." >&2; return 1; }
    create_or_attach_worktree
    state_set_execution "$model" "$reasoning"
    state_patch_status "INITIALIZING"
  fi
  ensure_worker_home
}

ensure_owned_worktree() {
  [[ -f "$state_file" ]] || { echo "No persistent session state for Issue #$issue. Use start." >&2; return 1; }
  [[ -d "$worktree" ]] || { echo "Persistent worktree is missing: $worktree" >&2; return 1; }
  [[ "$(git -C "$worktree" branch --show-current)" == "$branch" ]] || { echo "Unexpected branch in persistent worktree." >&2; return 1; }
  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    [[ "$(git -C "$worktree" rev-parse HEAD)" == "$(git rev-parse "origin/$branch")" ]] || { echo "Remote Change branch moved outside this session." >&2; return 1; }
  fi
  ensure_worker_home
  recover_active_turn_state
}

render_template() {
  local template="$1" issue_ctx="${2:-}" instruction="${3:-}"
  python3 - "$template" "$issue" "$contract" "$issue_ctx" "$instruction" <<'PY'
from pathlib import Path
import sys
template, issue, contract, issue_ctx, instruction = sys.argv[1:]
text = Path(template).read_text()
print(text.replace("{{ISSUE}}", issue).replace("{{CONTRACT}}", contract).replace("{{ISSUE_CONTEXT}}", issue_ctx).replace("{{LEAD_INSTRUCTION}}", instruction))
PY
}

read_instruction() {
  case "$1" in -) cat ;; *) [[ -f "$1" ]] || { echo "Instruction must be '-' or a file." >&2; return 1; }; cat "$1" ;; esac
}

validate_result() {
  jq -e 'type=="object" and (.status|IN("CANDIDATE_READY","CONTINUE","BLOCKED","NO_OP"))
    and (.summary|type=="string" and length>0) and (.verify|type=="string" and length>0)
    and (.blocker|type=="string" and length>0) and (keys|sort==["blocker","status","summary","verify"])' "$1" >/dev/null
}

run_candidate_v2() {
  local log="$1"
  for command in go npm docker; do command -v "$command" >/dev/null 2>&1 || { echo "Missing Candidate verifier: $command" >&2; return 1; }; done
  (
    set -euo pipefail
    cd "$worktree/backend"; test -z "$(gofmt -l .)"; go test ./...; go test -race ./...
    cd "$worktree/web"; npm ci; npm test; npm run build
    cd "$worktree"; docker compose config --quiet
  ) 2>&1 | tee "$log"
}

finalize_pushed_candidate() {
  local head pr remote
  head="$(jq -r '.candidate_head // ""' "$state_file")"
  [[ -n "$head" ]] || { echo "Pending Candidate has no candidate_head." >&2; return 1; }
  remote="$(remote_branch_head)" || { state_patch_status "PUBLISH_CONFIRMATION_PENDING" "$head"; return 1; }
  [[ "$remote" == "$head" ]] || { state_patch_status "PUBLISH_INCONCLUSIVE" "$head"; echo "Remote Candidate mismatch." >&2; return 1; }
  pr="$(ensure_pr)" || { state_patch_status "PUSHED_PENDING_GITHUB" "$head"; return 1; }
  verify_pr_head "$pr" "$head" || { state_patch_status "PUSHED_PENDING_GITHUB" "$head" "$pr"; return 1; }
  state_patch_status "CANDIDATE" "$head" "$pr"
  jq -n --arg status CANDIDATE_PUBLISHED --arg head "$head" --argjson pr "$pr" '{host_status:$status,head:$head,pr:$pr}'
}

publish_committed_candidate() {
  local head expected_remote remote push_rc remote_after
  head="$(jq -r '.candidate_head // ""' "$state_file")"
  expected_remote="$(jq -r '.candidate_remote_before // ""' "$state_file")"
  [[ -n "$head" ]] || { echo "No committed Candidate to publish." >&2; return 1; }
  [[ "$(git -C "$worktree" rev-parse HEAD)" == "$head" ]] || { state_patch_status "PUBLISH_INCONCLUSIVE" "$head"; echo "Local Candidate Head changed." >&2; return 1; }

  if remote="$(remote_branch_head)"; then
    if [[ "$remote" == "$head" ]]; then state_patch_status "PUSHED_PENDING_GITHUB" "$head"; finalize_pushed_candidate; return; fi
    if [[ "$remote" != "$expected_remote" ]]; then state_patch_status "PUBLISH_INCONCLUSIVE" "$head"; echo "Remote Change ref diverged before publish: expected=${expected_remote:-missing} actual=${remote:-missing}" >&2; return 1; fi
  fi

  set +e; "$repo_root/scripts/cddm-publish-branch.sh" "$worktree"; push_rc=$?; set -e
  if remote_after="$(remote_branch_head)"; then
    if [[ "$remote_after" == "$head" ]]; then state_patch_status "PUSHED_PENDING_GITHUB" "$head"; finalize_pushed_candidate; return; fi
    if [[ "$remote_after" != "$expected_remote" ]]; then state_patch_status "PUBLISH_INCONCLUSIVE" "$head"; echo "Remote Change ref diverged during publish: expected=${expected_remote:-missing} actual=${remote_after:-missing}" >&2; return 1; fi
    state_patch_status "COMMITTED_PENDING_PUSH" "$head"
    echo "Candidate push not accepted yet (rc=$push_rc); exact commit preserved for retry." >&2
    return 1
  fi
  state_patch_status "PUBLISH_CONFIRMATION_PENDING" "$head"
  echo "Candidate push outcome could not be confirmed; exact commit preserved." >&2
  return 1
}

reconcile_prepared_candidate() {
  local head parent local_head
  head="$(jq -r '.candidate_head // ""' "$state_file")"
  parent="$(jq -r '.candidate_parent // ""' "$state_file")"
  [[ -n "$head" && -n "$parent" ]] || { state_patch_status PUBLISH_INCONCLUSIVE; echo "Prepared Candidate state is incomplete." >&2; return 1; }
  git -C "$worktree" cat-file -e "$head^{commit}" 2>/dev/null || { state_patch_status PUBLISH_INCONCLUSIVE "$head"; echo "Prepared Candidate commit object is missing." >&2; return 1; }
  local_head="$(git -C "$worktree" rev-parse HEAD)"
  if [[ "$local_head" == "$parent" ]]; then
    git -C "$worktree" reset --hard "$head" >/dev/null
  elif [[ "$local_head" != "$head" ]]; then
    state_patch_status PUBLISH_INCONCLUSIVE "$head"
    echo "Local branch moved outside prepared Candidate: parent=$parent candidate=$head actual=$local_head" >&2
    return 1
  fi
  state_patch_status COMMITTED_PENDING_PUSH "$head"
  publish_committed_candidate
}

reconcile_pending_candidate() {
  local status
  status="$(jq -r '.status // ""' "$state_file")"
  case "$status" in
    COMMIT_PREPARED) reconcile_prepared_candidate ;;
    COMMITTED_PENDING_PUSH|PUBLISH_CONFIRMATION_PENDING) publish_committed_candidate ;;
    PUSHED_PENDING_GITHUB) finalize_pushed_candidate ;;
    PUBLISH_INCONCLUSIVE) echo "Candidate publication is inconclusive; Web Lead reconciliation required." >&2; return 1 ;;
  esac
}

commit_and_publish_candidate() {
  local result_file="$1" v2_log="$2" head parent tree remote_before v2_rc
  if [[ -z "$(git -C "$worktree" status --porcelain)" ]]; then
    state_patch_status INVALID_CANDIDATE_READY
    state_set_result_disposition "$result_file" 1
    echo "CANDIDATE_READY produced no file changes; use NO_OP." >&2
    return 1
  fi
  set +e
  run_strict run_candidate_v2 "$v2_log"
  v2_rc=$?
  set -e
  if [[ $v2_rc -ne 0 ]]; then
    state_patch_status V2_FAILED
    state_set_result_disposition "$result_file" 4
    echo "Host V2 failed; no Candidate published." >&2
    return 4
  fi
  git -C "$worktree" diff --check
  git -C "$worktree" add -A
  git -C "$worktree" diff --cached --check
  parent="$(git -C "$worktree" rev-parse HEAD)"
  tree="$(git -C "$worktree" write-tree)"
  head="$(printf 'Implement Issue #%s\n' "$issue" | git -C "$worktree" commit-tree "$tree" -p "$parent")"
  remote_before=""
  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then remote_before="$(git rev-parse "origin/$branch")"; fi
  state_set_prepared_candidate "$result_file" "$head" "$parent" "$remote_before"
  state_set_result_disposition "$result_file" 0
  git -C "$worktree" reset --hard "$head" >/dev/null
  state_patch_status COMMITTED_PENDING_PUSH "$head"
  publish_committed_candidate
}

comment_marker_present() {
  local target="$1" marker="$2" comments
  comments="$(gh api --paginate "repos/$repo_slug/issues/$target/comments" --jq '.[].body')" || return 2
  if grep -Fq "$marker" <<<"$comments"; then
    return 0
  fi
  return 1
}

persist_blocker() {
  local result_file="$1" pr target marker body rc
  marker="<!-- cddm-blocker-result:$(basename "$result_file") -->"
  body="$marker

## CDDM WebLead 3.0 Blocker

$(jq -r '"SUMMARY: " + .summary + "\nBLOCKER: " + .blocker' "$result_file")"

  if comment_marker_present "$issue" "$marker"; then
    return 0
  else
    rc=$?
  fi
  [[ $rc -eq 1 ]] || return "$rc"

  pr="$(find_pr_for_branch)"
  if [[ -n "$pr" ]]; then
    if comment_marker_present "$pr" "$marker"; then
      return 0
    else
      rc=$?
    fi
    [[ $rc -eq 1 ]] || return "$rc"
    target="$pr"
  else
    target="$issue"
  fi

  gh api "repos/$repo_slug/issues/$target/comments" -f body="$body" >/dev/null
}

dispatch_result_file() {
  local result_file="$1" v2_log="$2" result_status last_result last_result_rc candidate_result
  validate_result "$result_file" || return 13
  result_status="$(jq -r '.status' "$result_file")"
  last_result="$(jq -r '.last_result // ""' "$state_file")"
  last_result_rc="$(jq -r '.last_result_rc // 0' "$state_file")"

  # A result path is the durable identity of one Worker turn. Repeated statuses
  # are never evidence that a different result has already been consumed.
  [[ "$last_result" != "$result_file" ]] || return "$last_result_rc"

  case "$result_status" in
    CANDIDATE_READY)
      candidate_result="$(jq -r '.candidate_result // ""' "$state_file")"
      if [[ "$candidate_result" == "$result_file" ]]; then
        state_set_result_disposition "$result_file" 0
        reconcile_pending_candidate
        return
      fi
      commit_and_publish_candidate "$result_file" "$v2_log"
      ;;
    CONTINUE)
      state_patch_status CONTINUE
      state_set_result_disposition "$result_file" 0
      cat "$result_file"
      ;;
    BLOCKED)
      state_patch_status BLOCKER_PENDING_GITHUB
      persist_blocker "$result_file"
      state_patch_status BLOCKED
      state_set_result_disposition "$result_file" 0
      cat "$result_file"
      ;;
    NO_OP)
      if [[ -n "$(git -C "$worktree" status --porcelain)" ]]; then
        state_patch_status INVALID_NO_OP
        state_set_result_disposition "$result_file" 14
        echo "NO_OP returned with file changes." >&2
        return 14
      fi
      state_patch_status NO_OP
      state_set_result_disposition "$result_file" 0
      cat "$result_file"
      ;;
  esac
}

codex_command() {
  local mode="$1" thread_id="$2" model="$3" reasoning="$4" result="$5" codex_home
  codex_home="${CODEX_HOME:-$HOME/.codex}"
  local -a base=(
    env -u GH_TOKEN -u GITHUB_TOKEN -u GITHUB_ENTERPRISE_TOKEN -u SSH_AUTH_SOCK -u GIT_ASKPASS -u SSH_ASKPASS
    "HOME=$worker_home" "XDG_CONFIG_HOME=$worker_home/.config" "XDG_CACHE_HOME=$worker_home/.cache" "XDG_DATA_HOME=$worker_home/.local/share"
    "GH_CONFIG_DIR=$worker_home/.config/gh" "GIT_CONFIG_GLOBAL=/dev/null" "CODEX_HOME=$codex_home"
    codex exec --strict-config --ignore-user-config --json -C "$worktree" -m "$model"
    -c "model_reasoning_effort=\"$reasoning\"" -c "default_permissions=\"$permission_profile\""
    --output-schema "$schema" --output-last-message "$result"
  )
  if [[ "$mode" == resume ]]; then "${base[@]}" resume "$thread_id" -; else "${base[@]}" -; fi
}

persist_live_thread_event() {
  local mode="$1" previous_thread="$2" model="$3" reasoning="$4" rotation_reason="$5" events="$6" found stored
  found="$(thread_from_events "$events")"
  [[ -n "$found" ]] || return 1
  stored="$(jq -r '.thread_id // ""' "$state_file")"
  case "$mode" in
    start)
      if [[ -z "$stored" ]]; then state_set_thread "$found" RUNNING; elif [[ "$stored" != "$found" ]]; then echo "Initial thread identity changed unexpectedly." >&2; return 2; fi
      ;;
    resume)
      [[ "$found" == "$previous_thread" ]] || { echo "Resume returned unexpected thread id." >&2; return 2; }
      ;;
    rotate)
      if [[ "$stored" == "$previous_thread" ]]; then
        [[ "$found" != "$previous_thread" ]] || return 1
        state_rotate_thread "$found" "$model" "$reasoning" "$rotation_reason"
      elif [[ "$stored" != "$found" ]]; then
        echo "Rotation thread identity changed unexpectedly." >&2; return 2
      fi
      ;;
  esac
  return 0
}

run_codex_turn() {
  local mode="$1" thread_id="$2" model="$3" reasoning="$4" prompt="$5" rotation_reason="${6:-}"
  local stamp events result v2_log prompt_file pid_file pid rc=0 event_rc dispatch_rc consumed thread_rc
  stamp="$(date +%s)-$$"
  events="$results_dir/issue-$issue-$mode-$stamp.jsonl"
  result="$results_dir/issue-$issue-$mode-$stamp.result.json"
  v2_log="$results_dir/issue-$issue-v2-$stamp.log"
  prompt_file="$results_dir/issue-$issue-$mode-$stamp.prompt.txt"
  pid_file="$results_dir/issue-$issue-$mode-$stamp.pid"
  printf '%s\n' "$prompt" >"$prompt_file"
  : >"$events"
  rm -f "$pid_file"
  state_set_active_intent "$mode" "$events" "$result" "$v2_log" "$pid_file" "$thread_id" "$rotation_reason" "$model" "$reasoning"

  (
    printf '%s\n' "$BASHPID" >"$pid_file"
    codex_command "$mode" "$thread_id" "$model" "$reasoning" "$result" <"$prompt_file" >"$events"
  ) &
  pid=$!
  state_attach_active_pid "$pid"

  while kill -0 "$pid" 2>/dev/null; do
    set +e; run_strict persist_live_thread_event "$mode" "$thread_id" "$model" "$reasoning" "$rotation_reason" "$events"; event_rc=$?; set -e
    [[ $event_rc -ne 2 ]] || { kill "$pid" 2>/dev/null || true; break; }
    [[ $event_rc -eq 0 ]] && break
    sleep 0.1
  done
  set +e; wait "$pid"; rc=$?; set -e
  set +e; run_strict persist_live_thread_event "$mode" "$thread_id" "$model" "$reasoning" "$rotation_reason" "$events"; event_rc=$?; set -e
  rm -f "$prompt_file" "$pid_file"
  [[ $event_rc -ne 2 ]] || { state_patch_status THREAD_MISMATCH; return 11; }

  if [[ $event_rc -ne 0 ]]; then
    case "$mode" in
      start) state_patch_status START_FAILED_NO_THREAD ;;
      rotate) state_patch_status ROTATE_FAILED_NO_THREAD ;;
      *) state_patch_status THREAD_MISMATCH ;;
    esac
    echo "No valid thread.started event for completed $mode turn." >&2
    return 10
  fi

  state_record_turn_for_result "$result"
  if [[ $rc -ne 0 ]]; then
    state_patch_status TURN_FAILED
    state_set_result_disposition "$result" "$rc"
    state_clear_active
    echo "Codex turn failed; worktree/session preserved." >&2
    return "$rc"
  fi
  if [[ ! -s "$result" ]]; then
    state_patch_status EMPTY_RESULT
    state_set_result_disposition "$result" 12
    state_clear_active
    echo "Codex produced no final result." >&2
    return 12
  fi
  if ! validate_result "$result"; then
    state_patch_status INVALID_RESULT
    state_set_result_disposition "$result" 13
    cat "$result" >&2
    state_clear_active
    return 13
  fi

  set +e
  run_strict reconcile_completed_turn_thread "$mode" "$events" "$thread_id" "$model" "$reasoning" "$rotation_reason"
  thread_rc=$?
  set -e
  [[ $thread_rc -eq 0 ]] || return 11

  set +e
  run_strict dispatch_result_file "$result" "$v2_log"
  dispatch_rc=$?
  set -e
  consumed="$(jq -r '.last_result // ""' "$state_file")"
  if [[ "$consumed" == "$result" ]]; then
    state_clear_active
  fi
  return "$dispatch_rc"
}

exit_after_recovered_turn() {
  if [[ "$recovered_turn_dispatched" == 1 ]]; then
    exit 0
  fi
}

if [[ -f "$state_file" ]]; then
  stored_contract="$(jq -r '.contract // ""' "$state_file")"
  [[ -z "$stored_contract" ]] || contract="$stored_contract"
  pending_status="$(jq -r '.status // ""' "$state_file")"
  case "$pending_status" in
    COMMIT_PREPARED|COMMITTED_PENDING_PUSH|PUBLISH_CONFIRMATION_PENDING|PUSHED_PENDING_GITHUB|PUBLISH_INCONCLUSIVE)
      set +e
      run_strict reconcile_pending_candidate
      pending_rc=$?
      set -e
      if [[ $pending_rc -ne 0 && "$command_name" != status ]]; then
        echo "Pending Candidate reconciliation must succeed before another Codex turn." >&2
        exit 1
      fi
      ;;
  esac
fi

case "$command_name" in
  start)
    model="${1:-gpt-5.6-terra}"; reasoning="${2:-medium}"
    ensure_start_runtime "$model" "$reasoning"
    exit_after_recovered_turn
    thread_now="$(jq -r '.thread_id // ""' "$state_file")"
    [[ -z "$thread_now" ]] || { echo "Initial thread already established; use resume/status." >&2; exit 1; }
    prompt="$(render_template "$start_template" "$(issue_context)" "")"
    run_codex_turn start "" "$model" "$reasoning" "$prompt"
    ;;
  resume)
    [[ $# -ge 1 ]] || { usage >&2; exit 2; }
    source="$1"; shift; ensure_owned_worktree
    exit_after_recovered_turn
    thread_id="$(jq -r '.thread_id // ""' "$state_file")"; [[ -n "$thread_id" ]] || { echo "Persistent thread_id missing." >&2; exit 1; }
    stored_model="$(jq -r '.model' "$state_file")"; stored_reasoning="$(jq -r '.reasoning' "$state_file")"
    model="${1:-$stored_model}"; reasoning="${2:-$stored_reasoning}"; instruction="$(read_instruction "$source")"; [[ -n "$instruction" ]] || { echo "Resume instruction is empty." >&2; exit 2; }
    state_set_execution "$model" "$reasoning"
    run_codex_turn resume "$thread_id" "$model" "$reasoning" "$(render_template "$resume_template" "" "$instruction")"
    ;;
  rotate)
    [[ $# -ge 1 ]] || { usage >&2; exit 2; }
    source="$1"; shift; ensure_owned_worktree
    exit_after_recovered_turn
    thread_id="$(jq -r '.thread_id // ""' "$state_file")"; [[ -n "$thread_id" ]] || { echo "Persistent thread_id missing." >&2; exit 1; }
    stored_model="$(jq -r '.model' "$state_file")"; stored_reasoning="$(jq -r '.reasoning' "$state_file")"
    model="${1:-$stored_model}"; reasoning="${2:-$stored_reasoning}"; instruction="$(read_instruction "$source")"; [[ -n "$instruction" ]] || { echo "Rotate instruction is empty." >&2; exit 2; }
    run_codex_turn rotate "$thread_id" "$model" "$reasoning" "$(render_template "$rotate_template" "" "$instruction")" "$instruction"
    ;;
  status)
    [[ -f "$state_file" ]] || { echo "No runtime state for Issue #$issue."; exit 0; }
    cat "$state_file"; echo
    if [[ -d "$worktree" ]]; then echo "WORKTREE_STATUS:"; git -C "$worktree" status --short; fi
    turns="$(jq -r '.thread_turn_count // 0' "$state_file")"; (( turns < 6 )) || echo "CONTEXT_BUDGET: current thread has $turns turns; Web Lead should compare resume value against rotation cost."
    pr="$(find_pr_for_branch)"; [[ -z "$pr" ]] || { echo "PR: #$pr"; gh pr view "$pr" --repo "$repo_slug" --json isDraft,headRefOid,baseRefOid,state --jq '{state:.state,isDraft:.isDraft,base:.baseRefOid,head:.headRefOid}'; }
    ;;
esac
