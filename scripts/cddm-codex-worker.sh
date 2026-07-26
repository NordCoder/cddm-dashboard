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

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
[[ "$(git branch --show-current)" == "main" ]] || { echo "Run from the controlling main checkout." >&2; exit 1; }
[[ -z "$(git status --porcelain)" ]] || { echo "Controlling main must be clean." >&2; exit 1; }

gh auth status >/dev/null 2>&1 || { echo "GitHub CLI is not authenticated. Run: gh auth login" >&2; exit 1; }
codex login status >/dev/null 2>&1 || { echo "Codex CLI is not authenticated. Run: codex login" >&2; exit 1; }
git config user.name >/dev/null || { echo "Git user.name is not configured." >&2; exit 1; }
git config user.email >/dev/null || { echo "Git user.email is not configured." >&2; exit 1; }

git fetch --prune origin --quiet
git merge --ff-only origin/main --quiet
local_main="$(git rev-parse HEAD)"
remote_main="$(git rev-parse origin/main)"
[[ "$local_main" == "$remote_main" ]] || { echo "Local main differs from origin/main; refusing to launch." >&2; exit 1; }

origin_url="$(git remote get-url origin)"
case "$origin_url" in
  "https://github.com/NordCoder/cddm-dashboard"|"https://github.com/NordCoder/cddm-dashboard.git"|"git@github.com:NordCoder/cddm-dashboard.git"|"ssh://git@github.com/NordCoder/cddm-dashboard.git") ;;
  *) echo "Unexpected canonical origin: $origin_url" >&2; exit 1 ;;
esac

rules_path="$repo_root/.codex/rules/default.rules"
[[ -f "$rules_path" ]] || { echo "Missing Codex rules: $rules_path" >&2; exit 1; }
codex execpolicy check --rules "$rules_path" -- git status >/dev/null

mkdir -p "$repo_root/.worktrees/results"
result_file="$repo_root/.worktrees/results/${activity}-${target}-$(date +%s)-$$.txt"
evidence_file="$result_file.evidence.md"
worktree=""
review_head=""
review_base=""
cleanup_review=false

cleanup() {
  if [[ "$cleanup_review" == true && -n "$worktree" && -d "$worktree" ]]; then
    git worktree remove --force "$worktree" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

issue_context() {
  gh issue view "$1" --repo "$repo_slug" --json title,body --jq '"ISSUE TITLE: " + .title + "\n\nISSUE BODY:\n" + (.body // "")'
}

find_pr_for_branch() {
  gh pr list --repo "$repo_slug" --head "$1" --state open --json number --jq '.[0].number // empty'
}

ensure_pr() {
  local issue="$1" branch="$2" pr title
  pr="$(find_pr_for_branch "$branch")"
  if [[ -z "$pr" ]]; then
    title="$(gh issue view "$issue" --repo "$repo_slug" --json title --jq '.title')"
    gh pr create --repo "$repo_slug" --draft --base main --head "$branch" --title "$title" --body "Closes #$issue" >/dev/null
    pr="$(find_pr_for_branch "$branch")"
  fi
  [[ -n "$pr" ]] || { echo "Unable to resolve/create PR for $branch" >&2; return 1; }
  printf '%s' "$pr"
}

append_host_metadata() {
  local destination="$1" head="${2:-}" pr="${3:-}" reviewed="${4:-}"
  {
    echo "## CDDM Worker Result"
    echo
    cat "$result_file"
    [[ -n "$head" ]] && { echo; echo "CANDIDATE: $head"; }
    [[ -n "$reviewed" ]] && { echo; echo "REVIEWED_HEAD: $reviewed"; }
    [[ -n "$pr" ]] && echo "PR: #$pr"
  } > "$destination"
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
}

field() {
  local name="$1"
  sed -n "s/^${name}:[[:space:]]*//p" "$result_file" | head -n 1
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
  prompt+=$'\n\nHOST CANDIDATE CONTEXT:\nBASE SHA: '"$review_base"$'\nHEAD SHA: '"$review_head"
else
  issue="$target"
  branch="change/$issue"
  worktree="$repo_root/.worktrees/issue-$issue"

  if [[ -d "$worktree" ]]; then
    [[ "$(git -C "$worktree" rev-parse --abbrev-ref HEAD)" == "$branch" ]] || { echo "Unexpected branch in $worktree" >&2; exit 1; }
  else
    if git show-ref --verify --quiet "refs/heads/$branch"; then
      if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
        if git merge-base --is-ancestor "$branch" "origin/$branch"; then
          git branch -f "$branch" "origin/$branch" >/dev/null
        elif ! git merge-base --is-ancestor "origin/$branch" "$branch"; then
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
  fi

  prompt="$(sed "s/{{ISSUE}}/$issue/g" "$repo_root/.codex/prompts/$template")"
  prompt+=$'\n\nHOST ISSUE CONTEXT:\n'"$(issue_context "$issue")"
fi

set +e
printf '%s\n' "$prompt" | codex exec --ephemeral \
  -C "$worktree" \
  --sandbox workspace-write \
  -m "$model" \
  -c "model_reasoning_effort=\"$reasoning\"" \
  --output-last-message "$result_file" \
  -
worker_rc=$?
set -e

[[ $worker_rc -eq 0 ]] || { echo "Codex Worker failed with exit code $worker_rc" >&2; [[ -s "$result_file" ]] && cat "$result_file"; exit "$worker_rc"; }
[[ -s "$result_file" ]] || { echo "Worker produced no final result." >&2; exit 1; }
expected_activity="$(printf '%s' "$activity" | tr '[:lower:]-' '[:upper:]_')"
[[ "$(field ACTIVITY)" == "$expected_activity" ]] || { echo "Invalid Worker result activity." >&2; cat "$result_file" >&2; exit 1; }

if [[ "$activity" == "review" ]]; then
  [[ -z "$(git -C "$worktree" status --porcelain)" ]] || { echo "Reviewer modified the exact Candidate; verdict discarded." >&2; exit 1; }
  current_base="$(gh pr view "$pr" --repo "$repo_slug" --json baseRefOid --jq '.baseRefOid')"
  current_head="$(gh pr view "$pr" --repo "$repo_slug" --json headRefOid --jq '.headRefOid')"
  [[ "$current_base" == "$review_base" && "$current_head" == "$review_head" ]] || { echo "PR Base/Head changed during review; verdict discarded." >&2; exit 3; }
  append_host_metadata "$evidence_file" "" "$pr" "$review_head"
  gh pr comment "$pr" --repo "$repo_slug" --body-file "$evidence_file" >/dev/null
  cat "$result_file"
  exit 0
fi

status="$(field STATUS)"
pr="$(find_pr_for_branch "$branch")"
dirty=false
[[ -n "$(git -C "$worktree" status --porcelain)" ]] && dirty=true

case "$activity:$status" in
  shape:READY|shape:DECISION_REQUIRED|shape:DISCOVERY_REQUIRED)
    if [[ "$dirty" == true ]]; then
      commit_changes "Shape Issue #$issue"
    fi
    ahead_count="$(git -C "$worktree" rev-list --count "origin/main..HEAD")"
    if [[ "$ahead_count" -gt 0 ]]; then
      publish_branch
      pr="$(ensure_pr "$issue" "$branch")"
    fi
    ;;
  implement:DONE)
    [[ "$dirty" == true ]] || { echo "IMPLEMENT:DONE produced no file changes; return NO-OP when no implementation change is required." >&2; exit 1; }
    commit_changes "Implement Issue #$issue"
    publish_branch
    pr="$(ensure_pr "$issue" "$branch")"
    gh pr ready "$pr" --repo "$repo_slug" >/dev/null
    ;;
  implement:BLOCKED)
    ;;
  implement:NO-OP)
    [[ "$dirty" == false ]] || { echo "NO-OP result left file changes; refusing persistence." >&2; exit 1; }
    ;;
  investigate:RESOLVED|investigate:BLOCKED|investigate:NO_DEFECT)
    [[ "$dirty" == false ]] || { echo "Investigation modified files; refusing persistence." >&2; exit 1; }
    ;;
  fix-ci:FIXED)
    [[ "$dirty" == true ]] || { echo "FIX_CI:FIXED produced no file changes; use INFRA_FAILURE/INCONCLUSIVE when no correction is required." >&2; exit 1; }
    commit_changes "Fix CI for Issue #$issue"
    publish_branch
    pr="$(ensure_pr "$issue" "$branch")"
    ;;
  fix-ci:INFRA_FAILURE|fix-ci:BLOCKED|fix-ci:INCONCLUSIVE)
    [[ "$dirty" == false ]] || { echo "Non-fix CI result left file changes; refusing persistence." >&2; exit 1; }
    ;;
  *)
    echo "Unexpected result status '$status' for activity '$activity'." >&2
    cat "$result_file" >&2
    exit 1
    ;;
esac

candidate=""
if [[ -n "$pr" ]]; then
  candidate="$(git -C "$worktree" rev-parse HEAD)"
  append_host_metadata "$evidence_file" "$candidate" "$pr" ""
  gh pr comment "$pr" --repo "$repo_slug" --body-file "$evidence_file" >/dev/null
else
  append_host_metadata "$evidence_file" "" "" ""
  gh issue comment "$issue" --repo "$repo_slug" --body-file "$evidence_file" >/dev/null
fi

cat "$result_file"
