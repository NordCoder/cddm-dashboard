#!/usr/bin/env bash
set -euo pipefail

repo_slug="NordCoder/cddm-dashboard"

usage() {
  cat <<'EOF'
Usage:
  scripts/cddm-codex-worker.sh shape <issue> [model] [reasoning]
  scripts/cddm-codex-worker.sh implement <issue> [model] [reasoning]
  scripts/cddm-codex-worker.sh investigate <issue> [model] [reasoning]
  scripts/cddm-codex-worker.sh fix-ci <issue> [model] [reasoning]
  scripts/cddm-codex-worker.sh review <pr> [model] [reasoning]

Defaults:
  shape        gpt-5.6-sol   medium
  implement    gpt-5.6-terra medium
  investigate  gpt-5.6-terra medium
  fix-ci       gpt-5.6-terra medium
  review       gpt-5.6-terra medium
EOF
}

[[ $# -ge 2 ]] || { usage >&2; exit 2; }
activity="$1"
target="$2"
model="${3:-}"
reasoning="${4:-}"

case "$activity" in
  shape) model="${model:-gpt-5.6-sol}"; reasoning="${reasoning:-medium}"; template="shape.md" ;;
  implement) model="${model:-gpt-5.6-terra}"; reasoning="${reasoning:-medium}"; template="implement.md" ;;
  investigate) model="${model:-gpt-5.6-terra}"; reasoning="${reasoning:-medium}"; template="investigate.md" ;;
  fix-ci) model="${model:-gpt-5.6-terra}"; reasoning="${reasoning:-medium}"; template="fix-ci.md" ;;
  review) model="${model:-gpt-5.6-terra}"; reasoning="${reasoning:-medium}"; template="review.md" ;;
  *) echo "Unsupported activity: $activity" >&2; usage >&2; exit 2 ;;
esac

[[ "$target" =~ ^[0-9]+$ ]] || { echo "Issue/PR target must be numeric." >&2; exit 2; }

for command in git gh codex; do
  command -v "$command" >/dev/null 2>&1 || { echo "Missing required command: $command" >&2; exit 1; }
done
if [[ "$activity" == "implement" || "$activity" == "fix-ci" ]]; then
  for command in go npm docker; do
    command -v "$command" >/dev/null 2>&1 || { echo "Missing Candidate verifier: $command" >&2; exit 1; }
  done
fi

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
[[ "$(git branch --show-current)" == "main" ]] || { echo "Run from the controlling main checkout." >&2; exit 1; }
[[ -z "$(git status --porcelain)" ]] || { echo "Controlling main must be clean." >&2; exit 1; }

gh auth status >/dev/null 2>&1 || { echo "GitHub CLI is not authenticated. Run: gh auth login" >&2; exit 1; }
codex login status >/dev/null 2>&1 || { echo "Codex CLI is not authenticated. Run: codex login" >&2; exit 1; }
git config user.name >/dev/null || { echo "Git user.name is not configured." >&2; exit 1; }
git config user.email >/dev/null || { echo "Git user.email is not configured." >&2; exit 1; }

# Validate the canonical repository before any network fetch can modify host orchestration state.
origin_url="$(git remote get-url origin)"
case "$origin_url" in
  "https://github.com/NordCoder/cddm-dashboard"|"https://github.com/NordCoder/cddm-dashboard.git"|"git@github.com:NordCoder/cddm-dashboard.git"|"ssh://git@github.com/NordCoder/cddm-dashboard.git") ;;
  *) echo "Unexpected canonical origin: $origin_url" >&2; exit 1 ;;
esac

git fetch --prune origin --quiet
git merge --ff-only origin/main --quiet
local_main="$(git rev-parse HEAD)"
remote_main="$(git rev-parse origin/main)"
[[ "$local_main" == "$remote_main" ]] || { echo "Local main differs from origin/main; refusing to launch." >&2; exit 1; }

rules_path="$repo_root/.codex/rules/default.rules"
[[ -f "$rules_path" ]] || { echo "Missing Codex rules: $rules_path" >&2; exit 1; }
codex execpolicy check --rules "$rules_path" -- git status >/dev/null

mkdir -p "$repo_root/.worktrees/results"
result_file="$repo_root/.worktrees/results/${activity}-${target}-$(date +%s)-$$.txt"
evidence_file="$result_file.evidence.md"
v2_log="$result_file.v2.log"
worker_gh_dir="$repo_root/.worktrees/results/gh-empty-${activity}-${target}-$$"
mkdir -p "$worker_gh_dir"
chmod 700 "$worker_gh_dir"

worktree=""
review_head=""
review_base=""
failing_head=""
pr=""
cleanup_review=false
published=false
published_head=""
marked_ready=false

cleanup() {
  rm -rf "$worker_gh_dir" >/dev/null 2>&1 || true
  if [[ "$cleanup_review" == true && -n "$worktree" && -d "$worktree" ]]; then
    git worktree remove --force "$worktree" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

issue_context() {
  gh issue view "$1" --repo "$repo_slug" --json title,body --jq '"ISSUE TITLE: " + .title + "\n\nISSUE BODY:\n" + (.body // "")'
}

pr_context() {
  gh pr view "$1" --repo "$repo_slug" --json title,body,baseRefOid,headRefOid --jq '"PR TITLE: " + .title + "\nBASE SHA: " + .baseRefOid + "\nHEAD SHA: " + .headRefOid + "\n\nPR BODY:\n" + (.body // "")'
}

pr_checks_context() {
  gh pr checks "$1" --repo "$repo_slug" 2>&1 || true
}

ci_failure_context() {
  local pr_number="$1" head="$2" run_id output
  echo "PR CHECKS:"
  pr_checks_context "$pr_number"
  echo
  echo "FAILED RUN LOGS (bounded):"
  while IFS= read -r run_id; do
    [[ -n "$run_id" ]] || continue
    echo
    echo "--- run $run_id ---"
    output="$(gh run view "$run_id" --repo "$repo_slug" --log-failed 2>&1 || true)"
    if [[ -z "$output" ]]; then
      output="$(gh run view "$run_id" --repo "$repo_slug" 2>&1 || true)"
    fi
    printf '%s\n' "$output" | sed -n '1,800p'
  done < <(gh run list --repo "$repo_slug" --commit "$head" --limit 10 --json databaseId,conclusion --jq '.[] | select(.conclusion != null and .conclusion != "success" and .conclusion != "skipped") | .databaseId' | head -n 2)
}

find_pr_for_branch() {
  gh pr list --repo "$repo_slug" --head "$1" --state open --json number --jq '.[0].number // empty'
}

ensure_pr() {
  local issue="$1" branch="$2" found title
  found="$(find_pr_for_branch "$branch")"
  if [[ -z "$found" ]]; then
    title="$(gh issue view "$issue" --repo "$repo_slug" --json title --jq '.title')"
    gh pr create --repo "$repo_slug" --draft --base main --head "$branch" --title "$title" --body "Closes #$issue" >/dev/null
    found="$(find_pr_for_branch "$branch")"
  fi
  [[ -n "$found" ]] || { echo "Unable to resolve/create PR for $branch" >&2; return 1; }
  printf '%s' "$found"
}

write_evidence() {
  local destination="$1" pr_number="$2"
  shift 2
  {
    echo "## CDDM Worker Result"
    echo
    cat "$result_file"
    while [[ $# -ge 2 ]]; do
      echo
      echo "$1: $2"
      shift 2
    done
    [[ -n "$pr_number" ]] && echo "PR: #$pr_number"
  } > "$destination"
}

write_host_gate_failure() {
  local destination="$1" pr_number="$2" status="$3"
  {
    echo "## CDDM Host Gate"
    echo
    echo "STATUS: $status"
    echo "ACTIVITY: $(printf '%s' "$activity" | tr '[:lower:]-' '[:upper:]_')"
    [[ -n "$worktree" ]] && echo "LOCAL_HEAD: $(git -C "$worktree" rev-parse HEAD)"
    if [[ -s "$v2_log" ]]; then
      echo
      echo "V2_LOG_TAIL:"
      echo '```text'
      tail -n 80 "$v2_log"
      echo '```'
    fi
  } > "$destination"
  if [[ -n "$pr_number" ]]; then
    gh pr comment "$pr_number" --repo "$repo_slug" --body-file "$destination" >/dev/null
  else
    gh issue comment "$target" --repo "$repo_slug" --body-file "$destination" >/dev/null
  fi
}

commit_changes() {
  local message="$1"
  [[ -n "$(git -C "$worktree" status --porcelain)" ]] || return 0
  git -C "$worktree" diff --check
  git -C "$worktree" add -A
  git -C "$worktree" diff --cached --check
  if ! git -C "$worktree" diff --cached --quiet; then
    git -C "$worktree" commit -m "$message" >/dev/null
  fi
}

publish_branch() {
  "$repo_root/scripts/cddm-publish-branch.sh" "$worktree"
  published=true
  published_head="$(git -C "$worktree" rev-parse HEAD)"
}

field_count() {
  local name="$1"
  awk -v prefix="$name:" 'index($0, prefix) == 1 { count++ } END { print count + 0 }' "$result_file"
}

field() {
  local name="$1"
  sed -n "s/^${name}:[[:space:]]*//p" "$result_file" | head -n 1
}

require_field() {
  local name="$1"
  [[ "$(field_count "$name")" == "1" ]] || { echo "Worker result must contain exactly one '$name:' field." >&2; return 1; }
  [[ -n "$(field "$name")" ]] || { echo "Worker result field '$name' must not be empty." >&2; return 1; }
}

validate_result() {
  local nonempty expected status verdict findings contract
  nonempty="$(awk 'NF { count++ } END { print count + 0 }' "$result_file")"
  require_field ACTIVITY
  [[ "$(field ACTIVITY)" == "$(printf '%s' "$activity" | tr '[:lower:]-' '[:upper:]_')" ]] || { echo "Invalid Worker ACTIVITY." >&2; return 1; }

  case "$activity" in
    shape)
      expected=6
      for name in STATUS CONTRACT DECISIONS DEPENDENCIES NEXT; do require_field "$name"; done
      status="$(field STATUS)"
      [[ "$status" =~ ^(READY|DECISION_REQUIRED|DISCOVERY_REQUIRED)$ ]] || { echo "Invalid SHAPE status: $status" >&2; return 1; }
      contract="$(field CONTRACT)"
      [[ "$contract" == .delivery/changes/*.md ]] || { echo "Invalid canonical Change Contract path: $contract" >&2; return 1; }
      ;;
    implement)
      expected=5
      for name in STATUS CHANGED VERIFY BLOCKER; do require_field "$name"; done
      status="$(field STATUS)"
      [[ "$status" =~ ^(DONE|BLOCKED|NO-OP)$ ]] || { echo "Invalid IMPLEMENT status: $status" >&2; return 1; }
      ;;
    investigate)
      expected=5
      for name in STATUS FACTS CONCLUSION NEXT; do require_field "$name"; done
      status="$(field STATUS)"
      [[ "$status" =~ ^(RESOLVED|BLOCKED|NO_DEFECT)$ ]] || { echo "Invalid INVESTIGATE status: $status" >&2; return 1; }
      ;;
    fix-ci)
      expected=6
      for name in STATUS CAUSE CHANGED VERIFY NEXT; do require_field "$name"; done
      status="$(field STATUS)"
      [[ "$status" =~ ^(FIXED|INFRA_FAILURE|BLOCKED|INCONCLUSIVE)$ ]] || { echo "Invalid FIX_CI status: $status" >&2; return 1; }
      ;;
    review)
      expected=3
      for name in VERDICT FINDINGS; do require_field "$name"; done
      verdict="$(field VERDICT)"
      findings="$(field FINDINGS)"
      [[ "$verdict" =~ ^(APPROVED|BLOCKING_FINDINGS|EVIDENCE_INSUFFICIENT)$ ]] || { echo "Invalid REVIEW verdict: $verdict" >&2; return 1; }
      if [[ "$verdict" == "APPROVED" ]]; then
        [[ "$findings" == "none" ]] || { echo "APPROVED review must use FINDINGS: none" >&2; return 1; }
      elif [[ "$verdict" == "BLOCKING_FINDINGS" ]]; then
        [[ "$findings" != "none" ]] || { echo "BLOCKING_FINDINGS review must contain a bounded finding." >&2; return 1; }
      fi
      ;;
  esac

  [[ "$nonempty" == "$expected" ]] || { echo "Worker result contains unexpected/multiline fields; expected $expected non-empty schema lines, got $nonempty." >&2; return 1; }
}

sync_change_worktree() {
  local branch="$1" tree="$2" local_sha remote_sha
  [[ -z "$(git -C "$tree" status --porcelain)" ]] || {
    echo "Existing worktree $tree is dirty from unreconciled prior work; refusing to launch." >&2
    return 1
  }

  if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
    local_sha="$(git -C "$tree" rev-parse HEAD)"
    remote_sha="$(git rev-parse "origin/$branch")"
    if [[ "$local_sha" == "$remote_sha" ]]; then
      return 0
    fi
    if git merge-base --is-ancestor "$local_sha" "$remote_sha"; then
      git -C "$tree" merge --ff-only "$remote_sha" --quiet
      return 0
    fi
    if git merge-base --is-ancestor "$remote_sha" "$local_sha"; then
      return 0
    fi
    echo "Local and remote $branch diverged; Lead reconciliation required." >&2
    return 1
  fi
}

verify_pr_head() {
  local pr_number="$1" expected="$2" actual
  actual="$(gh pr view "$pr_number" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
  [[ "$actual" == "$expected" ]] || {
    echo "PR Head mismatch: expected $expected, GitHub $actual" >&2
    return 1
  }
}

reset_uncommitted_worker_changes() {
  git -C "$worktree" reset --hard HEAD >/dev/null
  git -C "$worktree" clean -fd >/dev/null
}

run_candidate_v2() {
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
  ) 2>&1 | tee "$v2_log"
}

persist_worker_failure() {
  local status="$1" found_pr=""
  if [[ "$activity" != "review" ]]; then
    found_pr="$(find_pr_for_branch "change/$target")"
    if [[ -n "$(git -C "$worktree" status --porcelain)" ]]; then
      commit_changes "WIP: Issue #$target worker failure"
    fi
    write_host_gate_failure "$evidence_file" "$found_pr" "$status"
  fi
}

if [[ "$activity" == "review" ]]; then
  pr="$target"
  review_base="$(gh pr view "$pr" --repo "$repo_slug" --json baseRefOid --jq '.baseRefOid')"
  review_head="$(gh pr view "$pr" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
  [[ -n "$review_base" && -n "$review_head" ]] || { echo "Unable to resolve PR Base/Head." >&2; exit 1; }

  git fetch origin "pull/$pr/head" --quiet
  fetched_head="$(git rev-parse FETCH_HEAD)"
  [[ "$fetched_head" == "$review_head" ]] || { echo "PR Head changed during review setup; rerun review." >&2; exit 3; }

  worktree="$repo_root/.worktrees/review-pr-$pr-${review_head:0:12}-$$"
  git worktree add --detach "$worktree" "$review_head" >/dev/null
  cleanup_review=true
  [[ "$(git -C "$worktree" rev-parse HEAD)" == "$review_head" ]] || { echo "Review Head mismatch." >&2; exit 1; }
  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || { echo "Fresh review worktree is dirty." >&2; exit 1; }

  prompt="$(sed "s/{{PR}}/$pr/g" "$repo_root/.codex/prompts/$template")"
  prompt+=$'\n\nHOST CANDIDATE CONTEXT:\n'"$(pr_context "$pr")"$'\n\nPR CHECKS:\n'"$(pr_checks_context "$pr")"
else
  issue="$target"
  branch="change/$issue"
  worktree="$repo_root/.worktrees/issue-$issue"

  if [[ -d "$worktree" ]]; then
    [[ "$(git -C "$worktree" rev-parse --abbrev-ref HEAD)" == "$branch" ]] || { echo "Unexpected branch in $worktree" >&2; exit 1; }
    sync_change_worktree "$branch" "$worktree"
  else
    if git show-ref --verify --quiet "refs/heads/$branch"; then
      if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
        local_sha="$(git rev-parse "$branch")"
        remote_sha="$(git rev-parse "origin/$branch")"
        if git merge-base --is-ancestor "$local_sha" "$remote_sha"; then
          git branch -f "$branch" "$remote_sha" >/dev/null
        elif ! git merge-base --is-ancestor "$remote_sha" "$local_sha"; then
          echo "Local and remote $branch diverged; Lead reconciliation required." >&2
          exit 1
        fi
      fi
      git worktree add "$worktree" "$branch" >/dev/null
    elif git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
      git branch --track "$branch" "origin/$branch" >/dev/null
      git worktree add "$worktree" "$branch" >/dev/null
    else
      [[ "$activity" != "fix-ci" ]] || { echo "No existing branch $branch for CI repair." >&2; exit 1; }
      git worktree add -b "$branch" "$worktree" origin/main >/dev/null
    fi
    [[ -z "$(git -C "$worktree" status --porcelain)" ]] || { echo "New Change worktree is unexpectedly dirty." >&2; exit 1; }
  fi

  pr="$(find_pr_for_branch "$branch")"
  if [[ "$activity" == "fix-ci" ]]; then
    [[ -n "$pr" ]] || { echo "No open PR exists for $branch; cannot bind CI repair to a Candidate." >&2; exit 1; }
    failing_head="$(gh pr view "$pr" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
    local_head="$(git -C "$worktree" rev-parse HEAD)"
    remote_head="$(git rev-parse "origin/$branch")"
    [[ "$local_head" == "$failing_head" && "$remote_head" == "$failing_head" ]] || {
      echo "CI repair worktree is not the exact PR Candidate: local=$local_head remote=$remote_head PR=$failing_head" >&2
      exit 1
    }
  fi

  prompt="$(sed "s/{{ISSUE}}/$issue/g" "$repo_root/.codex/prompts/$template")"
  prompt+=$'\n\nHOST ISSUE CONTEXT:\n'"$(issue_context "$issue")"
  if [[ "$activity" == "fix-ci" ]]; then
    prompt+=$'\n\nHOST CI REPAIR CONTEXT:\nPR: #'"$pr"$'\nFAILING CANDIDATE HEAD: '"$failing_head"$'\n\n'"$(ci_failure_context "$pr" "$failing_head")"
  fi
fi

set +e
printf '%s\n' "$prompt" | env \
  -u GH_TOKEN \
  -u GITHUB_TOKEN \
  -u GH_ENTERPRISE_TOKEN \
  -u GITHUB_ENTERPRISE_TOKEN \
  -u SSH_AUTH_SOCK \
  GH_CONFIG_DIR="$worker_gh_dir" \
  GIT_TERMINAL_PROMPT=0 \
  GIT_ASKPASS=/bin/false \
  SSH_ASKPASS=/bin/false \
  codex exec --ephemeral \
    -C "$worktree" \
    --sandbox workspace-write \
    -m "$model" \
    -c 'sandbox_workspace_write.network_access=false' \
    -c "model_reasoning_effort=\"$reasoning\"" \
    --output-last-message "$result_file" \
    -
worker_rc=$?
set -e

if [[ $worker_rc -ne 0 ]]; then
  persist_worker_failure "WORKER_FAILED"
  echo "Codex Worker failed with exit code $worker_rc" >&2
  [[ -s "$result_file" ]] && cat "$result_file"
  exit "$worker_rc"
fi
[[ -s "$result_file" ]] || { persist_worker_failure "EMPTY_RESULT"; echo "Worker produced no final result." >&2; exit 1; }
if ! validate_result; then
  persist_worker_failure "INVALID_RESULT"
  cat "$result_file" >&2
  exit 1
fi

if [[ "$activity" == "review" ]]; then
  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || { echo "Reviewer modified the exact Candidate; verdict discarded." >&2; exit 1; }
  current_base="$(gh pr view "$pr" --repo "$repo_slug" --json baseRefOid --jq '.baseRefOid')"
  current_head="$(gh pr view "$pr" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
  [[ "$current_base" == "$review_base" && "$current_head" == "$review_head" ]] || { echo "PR Base/Head changed during review; verdict discarded." >&2; exit 3; }
  write_evidence "$evidence_file" "$pr" "REVIEWED_HEAD" "$review_head"
  gh pr comment "$pr" --repo "$repo_slug" --body-file "$evidence_file" >/dev/null
  cat "$result_file"
  exit 0
fi

status="$(field STATUS)"
dirty=false
[[ -n "$(git -C "$worktree" status --porcelain)" ]] && dirty=true

if [[ "$activity" == "fix-ci" ]]; then
  current_head="$(gh pr view "$pr" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
  if [[ "$current_head" != "$failing_head" ]]; then
    [[ "$dirty" == false ]] || reset_uncommitted_worker_changes
    echo "PR Head changed during CI repair: expected $failing_head, current $current_head; Worker result discarded." >&2
    exit 3
  fi
fi

case "$activity:$status" in
  shape:READY|shape:DECISION_REQUIRED|shape:DISCOVERY_REQUIRED)
    if [[ "$dirty" == true ]]; then
      commit_changes "Shape Issue #$issue"
    fi
    ahead_count="$(git -C "$worktree" rev-list --count "origin/main..HEAD")"
    if [[ "$ahead_count" -gt 0 ]]; then
      publish_branch
      pr="$(ensure_pr "$issue" "$branch")"
      verify_pr_head "$pr" "$published_head"
    fi
    ;;
  implement:DONE)
    [[ "$dirty" == true ]] || { echo "IMPLEMENT:DONE produced no file changes; return NO-OP when no implementation change is required." >&2; exit 1; }
    if ! run_candidate_v2; then
      commit_changes "WIP: Issue #$issue host V2 failed"
      write_host_gate_failure "$evidence_file" "$pr" "V2_FAILED"
      echo "Host Candidate V2 failed; no Candidate was published." >&2
      exit 4
    fi
    commit_changes "Implement Issue #$issue"
    publish_branch
    pr="$(ensure_pr "$issue" "$branch")"
    verify_pr_head "$pr" "$published_head"
    if [[ "$(gh pr view "$pr" --repo "$repo_slug" --json isDraft --jq '.isDraft')" == "true" ]]; then
      gh pr ready "$pr" --repo "$repo_slug" >/dev/null
      marked_ready=true
      if ! verify_pr_head "$pr" "$published_head"; then
        gh pr ready "$pr" --repo "$repo_slug" --undo >/dev/null
        echo "PR Head changed while marking Candidate ready; restored draft state." >&2
        exit 3
      fi
    fi
    ;;
  implement:BLOCKED)
    if [[ "$dirty" == true ]]; then
      commit_changes "WIP: Issue #$issue blocked"
    fi
    ;;
  implement:NO-OP)
    [[ "$dirty" == false ]] || { reset_uncommitted_worker_changes; echo "NO-OP result left file changes; discarded them and refused persistence." >&2; exit 1; }
    ;;
  investigate:RESOLVED|investigate:BLOCKED|investigate:NO_DEFECT)
    [[ "$dirty" == false ]] || { reset_uncommitted_worker_changes; echo "Investigation modified files; discarded them and refused persistence." >&2; exit 1; }
    ;;
  fix-ci:FIXED)
    [[ "$dirty" == true ]] || { echo "FIX_CI:FIXED produced no file changes; use INFRA_FAILURE/INCONCLUSIVE when no correction is required." >&2; exit 1; }
    if ! run_candidate_v2; then
      commit_changes "WIP: Issue #$issue CI repair V2 failed"
      write_host_gate_failure "$evidence_file" "$pr" "V2_FAILED"
      echo "Host Candidate V2 failed; corrected Candidate was not published." >&2
      exit 4
    fi
    commit_changes "Fix CI for Issue #$issue"
    publish_branch
    verify_pr_head "$pr" "$published_head"
    ;;
  fix-ci:INFRA_FAILURE|fix-ci:BLOCKED|fix-ci:INCONCLUSIVE)
    [[ "$dirty" == false ]] || { reset_uncommitted_worker_changes; echo "Non-fix CI result left file changes; discarded them and refused persistence." >&2; exit 1; }
    ;;
  *)
    echo "Unexpected result status '$status' for activity '$activity'." >&2
    cat "$result_file" >&2
    exit 1
    ;;
esac

if [[ "$published" == true ]]; then
  if ! verify_pr_head "$pr" "$published_head"; then
    if [[ "$marked_ready" == true ]]; then
      gh pr ready "$pr" --repo "$repo_slug" --undo >/dev/null
    fi
    echo "Published Candidate changed before evidence persistence; result not recorded." >&2
    exit 3
  fi
fi

if [[ "$activity" == "fix-ci" ]]; then
  if [[ "$status" == "FIXED" ]]; then
    write_evidence "$evidence_file" "$pr" "PREVIOUS_CANDIDATE" "$failing_head" "CANDIDATE" "$published_head"
  else
    current_head="$(gh pr view "$pr" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
    [[ "$current_head" == "$failing_head" ]] || { echo "PR Head changed before CI diagnosis persistence; result discarded." >&2; exit 3; }
    write_evidence "$evidence_file" "$pr" "DIAGNOSED_HEAD" "$failing_head"
  fi
elif [[ -n "$pr" ]]; then
  if [[ "$published" == true && "$activity" == "shape" ]]; then
    write_evidence "$evidence_file" "$pr" "HEAD" "$published_head"
  elif [[ "$published" == true ]]; then
    write_evidence "$evidence_file" "$pr" "CANDIDATE" "$published_head"
  else
    write_evidence "$evidence_file" "$pr"
  fi
else
  write_evidence "$evidence_file" ""
fi

if [[ -n "$pr" ]]; then
  gh pr comment "$pr" --repo "$repo_slug" --body-file "$evidence_file" >/dev/null
else
  gh issue comment "$issue" --repo "$repo_slug" --body-file "$evidence_file" >/dev/null
fi

cat "$result_file"
