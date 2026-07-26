#!/usr/bin/env bash
set -euo pipefail

repo_slug="NordCoder/cddm-dashboard"

usage() {
  cat <<'EOF'
Usage:
  scripts/cddm-codex-change.sh start  <issue> [model] [reasoning]
  scripts/cddm-codex-change.sh resume <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh status <issue>

Defaults:
  model     gpt-5.6-terra
  reasoning medium

Examples:
  scripts/cddm-codex-change.sh start 17
  printf '%s\n' 'Fix QA finding F1 without changing the approved contract.' \
    | scripts/cddm-codex-change.sh resume 17 -
  scripts/cddm-codex-change.sh status 17
EOF
}

[[ $# -ge 2 ]] || { usage >&2; exit 2; }
command_name="$1"
issue="$2"
shift 2

[[ "$issue" =~ ^[0-9]+$ ]] || { echo "Issue must be numeric." >&2; exit 2; }
case "$command_name" in
  start|resume|status) ;;
  *) echo "Unsupported command: $command_name" >&2; usage >&2; exit 2 ;;
esac

for command in git gh codex jq; do
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
schema="$repo_root/.codex/schemas/change-turn-result.json"
start_template="$repo_root/.codex/prompts/change-start.md"
resume_template="$repo_root/.codex/prompts/change-resume.md"

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
  local found title contract="$1"
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

CDDM WebLead 3.0: implementation uses one persistent Codex Change session. Web Lead owns WHAT + HARD HOW + QA; Worker owns GOAL + private Tasks + LITE HOW." \
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

write_state() {
  local thread_id="$1" model="$2" reasoning="$3" contract="$4" status="$5"
  local candidate_head="${6:-}" pr="${7:-}" tmp
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
    --arg candidate_head "$candidate_head" \
    --arg pr "$pr" \
    --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{
      version:$version,
      issue:($issue|tonumber),
      branch:$branch,
      worktree:$worktree,
      thread_id:$thread_id,
      model:$model,
      reasoning:$reasoning,
      contract:$contract,
      status:$status,
      candidate_head:(if $candidate_head == "" then null else $candidate_head end),
      pr:(if $pr == "" then null else ($pr|tonumber) end),
      updated_at:$updated_at
    }' > "$tmp"
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

update_state_execution() {
  local model="$1" reasoning="$2" tmp
  tmp="$state_file.tmp"
  jq \
    --arg model "$model" \
    --arg reasoning "$reasoning" \
    --arg updated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '.model=$model | .reasoning=$reasoning | .updated_at=$updated_at' \
    "$state_file" > "$tmp"
  mv "$tmp" "$state_file"
}

ensure_start_worktree() {
  [[ ! -f "$state_file" ]] || {
    echo "Persistent session already exists for Issue #$issue. Use resume/status." >&2
    return 1
  }
  [[ ! -d "$worktree" ]] || {
    echo "Worktree already exists without runtime ownership state: $worktree" >&2
    return 1
  }

  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    remote_head="$(git rev-parse "origin/$branch")"
    if git show-ref --verify --quiet "refs/heads/$branch"; then
      local_head="$(git rev-parse "$branch")"
      [[ "$local_head" == "$remote_head" ]] || {
        echo "Existing local $branch does not match origin/$branch and has no persistent runtime owner; refusing to start." >&2
        return 1
      }
    else
      git branch --track "$branch" "origin/$branch" >/dev/null
    fi
    git worktree add "$worktree" "$branch" >/dev/null
  elif git show-ref --verify --quiet "refs/heads/$branch"; then
    echo "Unpublished local $branch exists without persistent runtime state; Web Lead reconciliation required." >&2
    return 1
  else
    git worktree add -b "$branch" "$worktree" origin/main >/dev/null
  fi

  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || {
    echo "New Change worktree is unexpectedly dirty." >&2
    return 1
  }
}

ensure_resume_worktree() {
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
      echo "Remote Change branch moved outside this persistent session: local=$local_head remote=$remote_head. Web Lead reconciliation required." >&2
      return 1
    }
  fi
}

render_template() {
  local template="$1" contract="$2" issue_ctx="${3:-}" instruction="${4:-}"
  python3 - "$template" "$issue" "$contract" "$issue_ctx" "$instruction" <<'PY'
from pathlib import Path
import sys
template, issue, contract, issue_ctx, instruction = sys.argv[1:]
text = Path(template).read_text()
text = text.replace("{{ISSUE}}", issue)
text = text.replace("{{CONTRACT}}", contract)
text = text.replace("{{ISSUE_CONTEXT}}", issue_ctx)
text = text.replace("{{LEAD_INSTRUCTION}}", instruction)
print(text)
PY
}

validate_result() {
  jq -e '
    type == "object"
    and (.status | IN("CANDIDATE_READY","CONTINUE","BLOCKED","NO_OP"))
    and (.summary | type == "string" and length > 0)
    and (.verify | type == "string" and length > 0)
    and (.blocker | type == "string" and length > 0)
    and (keys | sort == ["blocker","status","summary","verify"])
  ' "$1" >/dev/null
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

commit_and_publish_candidate() {
  local contract="$1" result_file="$2" v2_log="$3"
  local pr head

  [[ -n "$(git -C "$worktree" status --porcelain)" ]] || {
    echo "CANDIDATE_READY produced no file changes; return NO_OP when no modification is needed." >&2
    return 1
  }

  if ! run_candidate_v2 "$v2_log"; then
    update_state_status "V2_FAILED"
    echo "Host V2 failed; no Candidate was committed or published." >&2
    return 4
  fi

  git -C "$worktree" diff --check
  git -C "$worktree" add -A
  git -C "$worktree" diff --cached --check
  git -C "$worktree" commit -m "Implement Issue #$issue" >/dev/null

  "$repo_root/scripts/cddm-publish-branch.sh" "$worktree"
  head="$(git -C "$worktree" rev-parse HEAD)"
  pr="$(ensure_pr "$contract")"
  verify_pr_head "$pr" "$head"

  gh pr comment "$pr" --repo "$repo_slug" --body "## CDDM WebLead 3.0 Candidate

HEAD: \`$head\`
HOST_V2: PASS
CHANGE_SESSION: persistent

Web Lead QA and exact-Head CI remain required before merge." >/dev/null

  update_state_status "CANDIDATE" "$head" "$pr"

  jq -n \
    --arg status "CANDIDATE_PUBLISHED" \
    --arg head "$head" \
    --argjson pr "$pr" \
    --slurpfile worker "$result_file" \
    '{host_status:$status, head:$head, pr:$pr, worker:$worker[0]}'
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

run_codex_turn() {
  local mode="$1" thread_id="$2" model="$3" reasoning="$4" prompt="$5"
  local stamp events result rc observed_thread status
  stamp="$(date +%s)-$$"
  events="$results_dir/issue-$issue-$mode-$stamp.jsonl"
  result="$results_dir/issue-$issue-$mode-$stamp.result.json"

  set +e
  if [[ "$mode" == "start" ]]; then
    printf '%s\n' "$prompt" | codex exec \
      --json \
      -C "$worktree" \
      -m "$model" \
      -c "model_reasoning_effort=\"$reasoning\"" \
      --output-schema "$schema" \
      --output-last-message "$result" \
      - > "$events"
  else
    printf '%s\n' "$prompt" | codex exec \
      --json \
      -C "$worktree" \
      -m "$model" \
      -c "model_reasoning_effort=\"$reasoning\"" \
      --output-schema "$schema" \
      --output-last-message "$result" \
      resume "$thread_id" - > "$events"
  fi
  rc=$?
  set -e

  observed_thread="$(jq -r 'select(.type == "thread.started") | .thread_id // empty' "$events" | head -n 1)"

  if [[ "$mode" == "start" ]]; then
    [[ -n "$observed_thread" ]] || {
      echo "Codex did not emit thread.started; persistent session cannot be established." >&2
      return 10
    }
    thread_id="$observed_thread"
  elif [[ -n "$observed_thread" && "$observed_thread" != "$thread_id" ]]; then
    echo "Resume returned unexpected thread id: expected=$thread_id observed=$observed_thread" >&2
    return 11
  fi

  if [[ $rc -ne 0 ]]; then
    if [[ "$mode" == "start" && -n "$thread_id" ]]; then
      write_state "$thread_id" "$model" "$reasoning" "$contract" "TURN_FAILED"
    else
      update_state_status "TURN_FAILED"
    fi
    echo "Codex turn failed with exit code $rc. Worktree/session preserved for Lead inspection." >&2
    return "$rc"
  fi

  [[ -s "$result" ]] || {
    if [[ "$mode" == "start" ]]; then
      write_state "$thread_id" "$model" "$reasoning" "$contract" "EMPTY_RESULT"
    else
      update_state_status "EMPTY_RESULT"
    fi
    echo "Codex produced no final result. Worktree/session preserved." >&2
    return 12
  }

  if ! validate_result "$result"; then
    if [[ "$mode" == "start" ]]; then
      write_state "$thread_id" "$model" "$reasoning" "$contract" "INVALID_RESULT"
    else
      update_state_status "INVALID_RESULT"
    fi
    echo "Invalid Worker result schema. Worktree/session preserved but no Candidate may be published." >&2
    cat "$result" >&2
    return 13
  fi

  if [[ "$mode" == "start" ]]; then
    write_state "$thread_id" "$model" "$reasoning" "$contract" "RUNNING"
  fi

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
        echo "NO_OP returned with file changes; refusing to discard or publish them automatically." >&2
        update_state_status "INVALID_NO_OP"
        return 14
      }
      update_state_status "NO_OP"
      cat "$result"
      ;;
  esac
}

contract="$(resolve_contract)"

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
    stored_thread="$(jq -r '.thread_id' "$state_file" 2>/dev/null || true)"
    stored_model="$(jq -r '.model' "$state_file" 2>/dev/null || true)"
    stored_reasoning="$(jq -r '.reasoning' "$state_file" 2>/dev/null || true)"
    stored_contract="$(jq -r '.contract' "$state_file" 2>/dev/null || true)"

    [[ -n "$stored_thread" && "$stored_thread" != "null" ]] || {
      echo "Persistent thread_id missing for Issue #$issue." >&2
      exit 1
    }

    model="${1:-$stored_model}"
    reasoning="${2:-$stored_reasoning}"
    contract="${stored_contract:-$contract}"

    case "$instruction_source" in
      -) lead_instruction="$(cat)" ;;
      *)
        [[ -f "$instruction_source" ]] || {
          echo "Resume instruction must be '-' for stdin or an existing file." >&2
          exit 2
        }
        lead_instruction="$(cat "$instruction_source")"
        ;;
    esac
    [[ -n "$lead_instruction" ]] || { echo "Resume instruction is empty." >&2; exit 2; }

    ensure_resume_worktree
    update_state_execution "$model" "$reasoning"
    prompt="$(render_template "$resume_template" "$contract" "" "$lead_instruction")"
    run_codex_turn "resume" "$stored_thread" "$model" "$reasoning" "$prompt"
    ;;

  status)
    [[ -f "$state_file" ]] || {
      echo "No runtime state for Issue #$issue."
      exit 0
    }
    cat "$state_file"
    echo
    echo "WORKTREE_STATUS:"
    git -C "$worktree" status --short
    pr="$(find_pr_for_branch)"
    if [[ -n "$pr" ]]; then
      echo
      echo "PR: #$pr"
      gh pr view "$pr" --repo "$repo_slug" --json isDraft,headRefOid,baseRefOid,state \
        --jq '{state:.state,isDraft:.isDraft,base:.baseRefOid,head:.headRefOid}'
    fi
    ;;
esac
