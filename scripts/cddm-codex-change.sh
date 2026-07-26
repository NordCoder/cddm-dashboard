#!/usr/bin/env bash
set -euo pipefail

repo_slug="NordCoder/cddm-dashboard"
permission_profile="cddm-worker"

usage() {
  cat <<'EOF'
Usage:
  scripts/cddm-codex-change.sh start  <issue> [model] [reasoning]
  scripts/cddm-codex-change.sh resume <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh rotate <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh status <issue>

Defaults:
  model     gpt-5.6-terra
  reasoning medium

Meaning:
  start   create/recover the Change worktree and establish the persistent Codex thread
  resume  continue the same thread and worktree
  rotate  keep the worktree but intentionally start a fresh thread
  status  show local runtime/session/Candidate state
EOF
}

[[ $# -ge 2 ]] || { usage >&2; exit 2; }
command_name="$1"
issue="$2"
shift 2

[[ "$issue" =~ ^[0-9]+$ ]] || { echo "Issue must be numeric." >&2; exit 2; }
case "$command_name" in
  start|resume|rotate|status) ;;
  *) echo "Unsupported command: $command_name" >&2; usage >&2; exit 2 ;;
esac

for command in git gh codex jq python3; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "Missing required command: $command" >&2
    exit 1
  }
done

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

[[ "$(git branch --show-current)" == "main" ]] || {
  echo "Run the host launcher from the controlling main checkout." >&2
  exit 1
}
[[ -z "$(git status --porcelain)" ]] || {
  echo "Controlling main must be clean." >&2
  exit 1
}

origin_url="$(git remote get-url origin)"
case "$origin_url" in
  "https://github.com/NordCoder/cddm-dashboard"|"https://github.com/NordCoder/cddm-dashboard.git"|"git@github.com:NordCoder/cddm-dashboard.git"|"ssh://git@github.com/NordCoder/cddm-dashboard.git") ;;
  *) echo "Unexpected canonical origin: $origin_url" >&2; exit 1 ;;
esac

gh auth status >/dev/null 2>&1 || {
  echo "GitHub CLI is not authenticated. Run: gh auth login" >&2
  exit 1
}
codex login status >/dev/null 2>&1 || {
  echo "Codex CLI is not authenticated. Run: codex login" >&2
  exit 1
}
git config user.name >/dev/null || { echo "Git user.name is not configured." >&2; exit 1; }
git config user.email >/dev/null || { echo "Git user.email is not configured." >&2; exit 1; }

git fetch --prune origin --quiet
git merge --ff-only origin/main --quiet
local_main="$(git rev-parse HEAD)"
remote_main="$(git rev-parse origin/main)"
[[ "$local_main" == "$remote_main" ]] || {
  echo "Local main differs from origin/main; refusing to launch." >&2
  exit 1
}

runtime_dir="$repo_root/.worktrees/runtime"
results_dir="$repo_root/.worktrees/results"
mkdir -p "$runtime_dir" "$results_dir"

state_file="$runtime_dir/issue-$issue.json"
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
  if [[ ${#matches[@]} -gt 1 ]]; then
    echo "Multiple Change Contracts found for Issue #$issue." >&2
    return 1
  fi
  if [[ ${#matches[@]} -eq 1 ]]; then
    printf '%s' "${matches[0]#"$repo_root"/}"
  else
    printf '%s' "none"
  fi
}

issue_context() {
  gh issue view "$issue" --repo "$repo_slug" --json title,body \
    --jq '"ISSUE TITLE: " + .title + "\n\nISSUE BODY:\n" + (.body // "")'
}

find_pr_for_branch() {
  gh pr list --repo "$repo_slug" --head "$branch" --state open \
    --json number --jq '.[0].number // empty'
}

ensure_pr() {
  local contract="$1" found title
  found="$(find_pr_for_branch)"
  if [[ -z "$found" ]]; then
    title="$(gh issue view "$issue" --repo "$repo_slug" --json title --jq '.title')"
    gh pr create \
      --repo "$repo_slug" \
      --draft \
      --base main \
      --head "$branch" \
      --title "$title" \
      --body "Closes #$issue

Canonical Change Contract: \`$contract\`

CDDM WebLead 3.0: Web Lead owns WHAT + HARD HOW + QA; implementation uses one persistent Codex Change session unless explicitly rotated." \
      >/dev/null
    found="$(find_pr_for_branch)"
  fi
  [[ -n "$found" ]] || {
    echo "Unable to resolve/create PR for $branch." >&2
    return 1
  }
  printf '%s' "$found"
}

verify_pr_head() {
  local pr="$1" expected="$2" actual
  actual="$(gh pr view "$pr" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
  [[ "$actual" == "$expected" ]] || {
    echo "PR Head mismatch: expected=$expected actual=$actual" >&2
    return 1
  }
}

remote_branch_head() {
  local ref="refs/heads/$branch"
  git ls-remote "$origin_url" "$ref" | awk 'NR == 1 { print $1 }'
}

ensure_worker_home() {
  mkdir -p "$worker_home/.config/gh" "$worker_home/.cache" "$worker_home/.local/share"
  local exclude_file
  exclude_file="$(git -C "$worktree" rev-parse --git-path info/exclude)"
  if ! grep -Fxq '.cddm-worker-home/' "$exclude_file" 2>/dev/null; then
    printf '%s\n' '.cddm-worker-home/' >> "$exclude_file"
  fi
}

write_initial_state() {
  local thread_id="$1" model="$2" reasoning="$3" contract="$4" status="$5" tmp
  tmp="$state_file.tmp"
  jq -n \
    --argjson version 3 \
    --arg issue "$issue" \
    --arg branch "$branch" \
    --arg worktree "$worktree" \
    --arg thread_id "$thread_id" \
    --arg model "$model" \
    --arg reasoning "$reasoning" \
    --arg contract "$contract" \
    --arg status "$status" \
    --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{version:$version,issue:($issue|tonumber),branch:$branch,worktree:$worktree,thread_id:$thread_id,model:$model,reasoning:$reasoning,contract:$contract,status:$status,thread_turn_count:0,total_turn_count:0,thread_generation:1,thread_history:[],candidate_head:null,pr:null,updated_at:$updated_at}' \
    > "$tmp"
  mv "$tmp" "$state_file"
}

update_state_status() {
  local status="$1" candidate_head="${2:-}" pr="${3:-}" tmp
  tmp="$state_file.tmp"
  jq \
    --arg status "$status" \
    --arg candidate_head "$candidate_head" \
    --arg pr "$pr" \
    --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.status=$status
     | .updated_at=$updated_at
     | .candidate_head=(if $candidate_head == "" then .candidate_head else $candidate_head end)
     | .pr=(if $pr == "" then .pr else ($pr|tonumber) end)' \
    "$state_file" > "$tmp"
  mv "$tmp" "$state_file"
}

set_thread_state() {
  local thread_id="$1" status="$2" tmp="$state_file.tmp"
  jq \
    --arg thread_id "$thread_id" \
    --arg status "$status" \
    --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.thread_id=$thread_id | .status=$status | .updated_at=$updated_at' \
    "$state_file" > "$tmp"
  mv "$tmp" "$state_file"
}

update_state_execution() {
  local model="$1" reasoning="$2" tmp="$state_file.tmp"
  jq --arg model "$model" --arg reasoning "$reasoning" --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.model=$model | .reasoning=$reasoning | .updated_at=$updated_at' "$state_file" > "$tmp"
  mv "$tmp" "$state_file"
}

record_turn() {
  local tmp="$state_file.tmp"
  jq --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.thread_turn_count=((.thread_turn_count // 0) + 1)
     | .total_turn_count=((.total_turn_count // 0) + 1)
     | .updated_at=$updated_at' \
    "$state_file" > "$tmp"
  mv "$tmp" "$state_file"
}

rotate_thread_state() {
  local new_thread="$1" model="$2" reasoning="$3" reason="$4" tmp="$state_file.tmp"
  jq \
    --arg new_thread "$new_thread" \
    --arg model "$model" \
    --arg reasoning "$reasoning" \
    --arg reason "$reason" \
    --arg rotated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.thread_history=((.thread_history // []) + [{thread_id:.thread_id,model:.model,reasoning:.reasoning,turn_count:(.thread_turn_count // 0),rotated_at:$rotated_at,reason:$reason}])
     | .thread_id=$new_thread
     | .model=$model
     | .reasoning=$reasoning
     | .thread_turn_count=0
     | .thread_generation=((.thread_generation // 1) + 1)
     | .status="ROTATED"
     | .updated_at=$rotated_at' \
    "$state_file" > "$tmp"
  mv "$tmp" "$state_file"
}

ensure_start_worktree() {
  if [[ -f "$state_file" ]]; then
    local existing_thread existing_status
    existing_thread="$(jq -r '.thread_id // ""' "$state_file")"
    existing_status="$(jq -r '.status // ""' "$state_file")"
    if [[ -z "$existing_thread" && "$existing_status" =~ ^(INITIALIZING|START_FAILED_NO_THREAD)$ ]]; then
      ensure_owned_worktree
      return
    fi
    echo "Persistent session already exists for Issue #$issue. Use resume/rotate/status." >&2
    return 1
  fi

  [[ ! -d "$worktree" ]] || {
    echo "Worktree exists without runtime ownership state: $worktree" >&2
    return 1
  }

  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    remote_head="$(git rev-parse "origin/$branch")"
    if git show-ref --verify --quiet "refs/heads/$branch"; then
      local_head="$(git rev-parse "$branch")"
      [[ "$local_head" == "$remote_head" ]] || {
        echo "Existing local $branch does not match origin/$branch and has no runtime owner." >&2
        return 1
      }
    else
      git branch --track "$branch" "origin/$branch" >/dev/null
    fi
    git worktree add "$worktree" "$branch" >/dev/null
  elif git show-ref --verify --quiet "refs/heads/$branch"; then
    echo "Unpublished local $branch exists without runtime ownership state; Web Lead reconciliation required." >&2
    return 1
  else
    git worktree add -b "$branch" "$worktree" origin/main >/dev/null
  fi

  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || {
    echo "New Change worktree is unexpectedly dirty." >&2
    return 1
  }
  ensure_worker_home
  write_initial_state "" "$model" "$reasoning" "$contract" "INITIALIZING"
}

ensure_owned_worktree() {
  [[ -f "$state_file" ]] || {
    echo "No persistent session state for Issue #$issue. Use start." >&2
    return 1
  }
  [[ -d "$worktree" ]] || {
    echo "Persistent worktree is missing: $worktree" >&2
    return 1
  }
  [[ "$(git -C "$worktree" branch --show-current)" == "$branch" ]] || {
    echo "Unexpected branch in persistent worktree." >&2
    return 1
  }

  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    local local_head remote_head
    local_head="$(git -C "$worktree" rev-parse HEAD)"
    remote_head="$(git rev-parse "origin/$branch")"
    [[ "$local_head" == "$remote_head" ]] || {
      echo "Remote Change branch moved outside this persistent session: local=$local_head remote=$remote_head." >&2
      return 1
    }
  fi
  ensure_worker_home
}

render_template() {
  local template="$1" contract="$2" issue_ctx="${3:-}" instruction="${4:-}"
  python3 - "$template" "$issue" "$contract" "$issue_ctx" "$instruction" <<'PY'
from pathlib import Path
import sys
template, issue, contract, issue_ctx, instruction = sys.argv[1:]
text = Path(template).read_text()
text = text.replace("{{ISSUE}}", issue).replace("{{CONTRACT}}", contract).replace("{{ISSUE_CONTEXT}}", issue_ctx).replace("{{LEAD_INSTRUCTION}}", instruction)
print(text)
PY
}

read_instruction() {
  local source="$1"
  case "$source" in
    -) cat ;;
    *)
      [[ -f "$source" ]] || { echo "Instruction must be '-' or a file." >&2; return 1; }
      cat "$source"
      ;;
  esac
}

validate_result() {
  jq -e 'type == "object"
    and (.status | IN("CANDIDATE_READY","CONTINUE","BLOCKED","NO_OP"))
    and (.summary|type=="string" and length>0)
    and (.verify|type=="string" and length>0)
    and (.blocker|type=="string" and length>0)
    and (keys|sort == ["blocker","status","summary","verify"])' "$1" >/dev/null
}

run_candidate_v2() {
  local log="$1"
  for command in go npm docker; do
    command -v "$command" >/dev/null 2>&1 || {
      echo "Missing Candidate verifier: $command" >&2
      return 1
    }
  done
  (
    set -euo pipefail
    cd "$worktree/backend"
    test -z "$(gofmt -l .)"
    go test ./...
    go test -race ./...

    cd "$worktree/web"
    npm ci
    npm test
    npm run build

    cd "$worktree"
    docker compose config --quiet
  ) 2>&1 | tee "$log"
}

finalize_pushed_candidate() {
  local contract="$1" head pr remote_head

  head="$(jq -r '.candidate_head // ""' "$state_file")"
  [[ -n "$head" ]] || {
    echo "Pending Candidate state has no candidate_head." >&2
    return 1
  }

  if ! remote_head="$(remote_branch_head)"; then
    update_state_status "PUBLISH_CONFIRMATION_PENDING" "$head"
    echo "Unable to confirm remote Change ref; Candidate bookkeeping remains pending." >&2
    return 1
  fi
  [[ "$remote_head" == "$head" ]] || {
    update_state_status "PUBLISH_INCONCLUSIVE" "$head"
    echo "Remote Change ref does not match pending Candidate: expected=$head actual=${remote_head:-missing}" >&2
    return 1
  }

  pr="$(ensure_pr "$contract")" || {
    update_state_status "PUSHED_PENDING_GITHUB" "$head"
    echo "Candidate is pushed but PR bookkeeping is pending." >&2
    return 1
  }
  verify_pr_head "$pr" "$head" || {
    update_state_status "PUSHED_PENDING_GITHUB" "$head" "$pr"
    return 1
  }

  update_state_status "CANDIDATE" "$head" "$pr"
  jq -n --arg status "CANDIDATE_PUBLISHED" --arg head "$head" --argjson pr "$pr" \
    '{host_status:$status,head:$head,pr:$pr}'
}

reconcile_pending_candidate() {
  [[ -f "$state_file" ]] || return 0
  local status
  status="$(jq -r '.status // ""' "$state_file")"
  case "$status" in
    PUSHED_PENDING_GITHUB|PUBLISH_CONFIRMATION_PENDING|PUBLISH_INCONCLUSIVE)
      echo "Reconciling pending Candidate publication for Issue #$issue..." >&2
      finalize_pushed_candidate "$contract"
      ;;
  esac
}

commit_and_publish_candidate() {
  local contract="$1" result_file="$2" v2_log="$3"
  local head push_rc remote_after

  [[ -n "$(git -C "$worktree" status --porcelain)" ]] || {
    echo "CANDIDATE_READY produced no file changes; use NO_OP." >&2
    return 1
  }

  if ! run_candidate_v2 "$v2_log"; then
    update_state_status "V2_FAILED"
    echo "Host V2 failed; no Candidate published." >&2
    return 4
  fi

  git -C "$worktree" diff --check
  git -C "$worktree" add -A
  git -C "$worktree" diff --cached --check
  git -C "$worktree" commit -m "Implement Issue #$issue" >/dev/null
  head="$(git -C "$worktree" rev-parse HEAD)"

  set +e
  "$repo_root/scripts/cddm-publish-branch.sh" "$worktree"
  push_rc=$?
  set -e

  if remote_after="$(remote_branch_head)"; then
    if [[ "$remote_after" == "$head" ]]; then
      update_state_status "PUSHED_PENDING_GITHUB" "$head"
      finalize_pushed_candidate "$contract"
      return
    fi

    if [[ $push_rc -ne 0 ]]; then
      git -C "$worktree" reset --mixed HEAD^ >/dev/null
      update_state_status "PUBLISH_FAILED"
      echo "Push failed and remote ref did not advance; local commit returned to Working State." >&2
      return 5
    fi

    update_state_status "PUBLISH_INCONCLUSIVE" "$head"
    echo "Push reported success but remote ref is unexpected; preserving local commit for reconciliation." >&2
    return 5
  fi

  update_state_status "PUBLISH_CONFIRMATION_PENDING" "$head"
  echo "Push outcome could not be confirmed; preserving local commit and pending Candidate state." >&2
  return 5
}

persist_blocker() {
  local result_file="$1" pr
  pr="$(find_pr_for_branch)"
  if [[ -n "$pr" ]]; then
    gh pr comment "$pr" --repo "$repo_slug" --body "## CDDM WebLead 3.0 Blocker

$(jq -r '"SUMMARY: " + .summary + "\nBLOCKER: " + .blocker' "$result_file")" >/dev/null
  else
    gh issue comment "$issue" --repo "$repo_slug" --body "## CDDM WebLead 3.0 Blocker

$(jq -r '"SUMMARY: " + .summary + "\nBLOCKER: " + .blocker' "$result_file")" >/dev/null
  fi
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
  if [[ "$mode" == "resume" ]]; then
    "${base[@]}" resume "$thread_id" -
  else
    "${base[@]}" -
  fi
}

run_codex_turn() {
  local mode="$1" thread_id="$2" model="$3" reasoning="$4" prompt="$5" rotation_reason="${6:-}"
  local stamp events result rc observed_thread status

  stamp="$(date +%s)-$$"
  events="$results_dir/issue-$issue-$mode-$stamp.jsonl"
  result="$results_dir/issue-$issue-$mode-$stamp.result.json"

  set +e
  printf '%s\n' "$prompt" | codex_command "$mode" "$thread_id" "$model" "$reasoning" "$result" > "$events"
  rc=$?
  set -e

  observed_thread="$(jq -r 'select(.type == "thread.started") | .thread_id // .thread.id // empty' "$events" | head -n 1)"

  if [[ "$mode" == "start" ]]; then
    if [[ -z "$observed_thread" ]]; then
      update_state_status "START_FAILED_NO_THREAD"
      echo "No thread.started event; owned worktree/state preserved for start retry." >&2
      return 10
    fi
    thread_id="$observed_thread"
    set_thread_state "$thread_id" "RUNNING"
  elif [[ "$mode" == "rotate" ]]; then
    [[ -n "$observed_thread" && "$observed_thread" != "$thread_id" ]] || {
      echo "Rotation did not establish a fresh thread." >&2
      return 11
    }
    thread_id="$observed_thread"
    rotate_thread_state "$thread_id" "$model" "$reasoning" "$rotation_reason"
  elif [[ -n "$observed_thread" && "$observed_thread" != "$thread_id" ]]; then
    echo "Resume returned unexpected thread id." >&2
    return 11
  fi

  record_turn

  if [[ $rc -ne 0 ]]; then
    update_state_status "TURN_FAILED"
    echo "Codex turn failed; worktree/session preserved." >&2
    return "$rc"
  fi
  [[ -s "$result" ]] || {
    update_state_status "EMPTY_RESULT"
    echo "Codex produced no final result." >&2
    return 12
  }
  validate_result "$result" || {
    update_state_status "INVALID_RESULT"
    cat "$result" >&2
    return 13
  }

  status="$(jq -r '.status' "$result")"
  case "$status" in
    CANDIDATE_READY)
      commit_and_publish_candidate "$contract" "$result" "$results_dir/issue-$issue-v2-$stamp.log"
      ;;
    CONTINUE)
      update_state_status "CONTINUE"
      cat "$result"
      ;;
    BLOCKED)
      update_state_status "BLOCKED"
      persist_blocker "$result"
      cat "$result"
      ;;
    NO_OP)
      [[ -z "$(git -C "$worktree" status --porcelain)" ]] || {
        update_state_status "INVALID_NO_OP"
        echo "NO_OP returned with file changes." >&2
        return 14
      }
      update_state_status "NO_OP"
      cat "$result"
      ;;
  esac
}

resolve_runtime_contract() {
  contract="$(resolve_contract)"
  if [[ -f "$state_file" ]]; then
    stored_contract="$(jq -r '.contract' "$state_file")"
    [[ -z "$stored_contract" || "$stored_contract" == "null" ]] || contract="$stored_contract"
  fi
}

resolve_runtime_contract

if [[ -f "$state_file" ]]; then
  pending_status="$(jq -r '.status // ""' "$state_file")"
  case "$pending_status" in
    PUSHED_PENDING_GITHUB|PUBLISH_CONFIRMATION_PENDING|PUBLISH_INCONCLUSIVE)
      if ! reconcile_pending_candidate; then
        if [[ "$command_name" != "status" ]]; then
          echo "Pending Candidate reconciliation must succeed before another Codex turn." >&2
          exit 1
        fi
      fi
      ;;
  esac
fi

case "$command_name" in
  start)
    model="${1:-gpt-5.6-terra}"
    reasoning="${2:-medium}"
    ensure_start_worktree
    issue_ctx="$(issue_context)"
    prompt="$(render_template "$start_template" "$contract" "$issue_ctx" "")"
    run_codex_turn "start" "" "$model" "$reasoning" "$prompt"
    ;;
  resume)
    [[ $# -ge 1 ]] || { usage >&2; exit 2; }
    instruction_source="$1"
    shift
    ensure_owned_worktree
    stored_thread="$(jq -r '.thread_id' "$state_file")"
    stored_model="$(jq -r '.model' "$state_file")"
    stored_reasoning="$(jq -r '.reasoning' "$state_file")"
    [[ -n "$stored_thread" && "$stored_thread" != "null" ]] || {
      echo "Persistent thread_id missing; use start to recover initialization." >&2
      exit 1
    }
    model="${1:-$stored_model}"
    reasoning="${2:-$stored_reasoning}"
    lead_instruction="$(read_instruction "$instruction_source")"
    [[ -n "$lead_instruction" ]] || { echo "Resume instruction is empty." >&2; exit 2; }
    update_state_execution "$model" "$reasoning"
    prompt="$(render_template "$resume_template" "$contract" "" "$lead_instruction")"
    run_codex_turn "resume" "$stored_thread" "$model" "$reasoning" "$prompt"
    ;;
  rotate)
    [[ $# -ge 1 ]] || { usage >&2; exit 2; }
    instruction_source="$1"
    shift
    ensure_owned_worktree
    old_thread="$(jq -r '.thread_id' "$state_file")"
    stored_model="$(jq -r '.model' "$state_file")"
    stored_reasoning="$(jq -r '.reasoning' "$state_file")"
    model="${1:-$stored_model}"
    reasoning="${2:-$stored_reasoning}"
    lead_instruction="$(read_instruction "$instruction_source")"
    [[ -n "$lead_instruction" ]] || { echo "Rotate instruction is empty." >&2; exit 2; }
    prompt="$(render_template "$rotate_template" "$contract" "" "$lead_instruction")"
    run_codex_turn "rotate" "$old_thread" "$model" "$reasoning" "$prompt" "$lead_instruction"
    ;;
  status)
    [[ -f "$state_file" ]] || { echo "No runtime state for Issue #$issue."; exit 0; }
    cat "$state_file"
    echo
    echo "WORKTREE_STATUS:"
    git -C "$worktree" status --short
    turn_count="$(jq -r '.thread_turn_count // 0' "$state_file")"
    if (( turn_count >= 6 )); then
      echo
      echo "CONTEXT_BUDGET: current thread has $turn_count turns; Web Lead should compare resume value against rotation cost."
    fi
    pr="$(find_pr_for_branch)"
    if [[ -n "$pr" ]]; then
      echo
      echo "PR: #$pr"
      gh pr view "$pr" --repo "$repo_slug" --json isDraft,headRefOid,baseRefOid,state \
        --jq '{state:.state,isDraft:.isDraft,base:.baseRefOid,head:.headRefOid}'
    fi
    ;;
esac
