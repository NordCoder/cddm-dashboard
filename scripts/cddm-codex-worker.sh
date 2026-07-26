#!/usr/bin/env bash
set -euo pipefail

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

The Web Lead may override model/reasoning per task risk and leverage.
EOF
}

[[ $# -ge 2 ]] || { usage >&2; exit 2; }

activity="$1"
target="$2"
model="${3:-}"
reasoning="${4:-}"

case "$activity" in
  shape)
    model="${model:-gpt-5.6-sol}"
    reasoning="${reasoning:-medium}"
    template="shape.md"
    ;;
  implement)
    model="${model:-gpt-5.6-terra}"
    reasoning="${reasoning:-medium}"
    template="implement.md"
    ;;
  investigate)
    model="${model:-gpt-5.6-terra}"
    reasoning="${reasoning:-medium}"
    template="investigate.md"
    ;;
  fix-ci)
    model="${model:-gpt-5.6-terra}"
    reasoning="${reasoning:-medium}"
    template="fix-ci.md"
    ;;
  review)
    model="${model:-gpt-5.6-terra}"
    reasoning="${reasoning:-medium}"
    template="review.md"
    ;;
  *)
    echo "Unsupported activity: $activity" >&2
    usage >&2
    exit 2
    ;;
esac

[[ "$target" =~ ^[0-9]+$ ]] || {
  echo "Issue/PR target must be a positive integer: $target" >&2
  exit 2
}

for command in git gh codex; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "Required command is not installed or not on PATH: $command" >&2
    exit 1
  }
done

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

control_branch="$(git branch --show-current)"
if [[ "$control_branch" != "main" ]]; then
  echo "Run the launcher from the clean controlling 'main' checkout, not '$control_branch'." >&2
  exit 1
fi

# Fail before spending a model call when credentials or command policy are invalid.
gh auth status >/dev/null 2>&1 || {
  echo "GitHub CLI is not authenticated. Run: gh auth login" >&2
  exit 1
}
codex login status >/dev/null 2>&1 || {
  echo "Codex CLI is not authenticated. Run: codex login" >&2
  exit 1
}

rules_path="$repo_root/.codex/rules/default.rules"
[[ -f "$rules_path" ]] || { echo "Missing Codex rules: $rules_path" >&2; exit 1; }
codex execpolicy check --rules "$rules_path" -- git status >/dev/null

# The controlling checkout is orchestration state only. Generated worktrees are ignored.
if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  echo "Controlling checkout has tracked local changes; refusing to derive worker state from it." >&2
  exit 1
fi

git fetch origin main --quiet
mkdir -p "$repo_root/.worktrees"

if [[ "$activity" == "review" ]]; then
  pr="$target"
  prompt_path="$repo_root/.codex/prompts/$template"
  [[ -f "$prompt_path" ]] || { echo "Missing prompt template: $prompt_path" >&2; exit 1; }

  head_sha="$(gh pr view "$pr" --json headRefOid --jq '.headRefOid')"
  [[ -n "$head_sha" && "$head_sha" != "null" ]] || {
    echo "Unable to resolve PR #$pr Head." >&2
    exit 1
  }

  git fetch origin "pull/$pr/head" --quiet

  worktree="$repo_root/.worktrees/review-pr-$pr"
  if [[ -d "$worktree" ]]; then
    existing_head="$(git -C "$worktree" rev-parse HEAD)"
    if [[ "$existing_head" != "$head_sha" ]]; then
      git worktree remove --force "$worktree"
    fi
  fi
  if [[ ! -d "$worktree" ]]; then
    git worktree add --detach "$worktree" "$head_sha" >/dev/null
  fi

  prompt="$(sed "s/{{PR}}/$pr/g" "$prompt_path")"
else
  issue="$target"
  prompt_path="$repo_root/.codex/prompts/$template"
  [[ -f "$prompt_path" ]] || { echo "Missing prompt template: $prompt_path" >&2; exit 1; }

  branch="change/$issue"
  worktree="$repo_root/.worktrees/issue-$issue"

  if [[ -d "$worktree" ]]; then
    current_branch="$(git -C "$worktree" branch --show-current)"
    [[ "$current_branch" == "$branch" ]] || {
      echo "Existing worktree $worktree is on '$current_branch', expected '$branch'." >&2
      exit 1
    }
  else
    if git show-ref --verify --quiet "refs/heads/$branch"; then
      git worktree add "$worktree" "$branch" >/dev/null
    elif git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
      git branch --track "$branch" "origin/$branch" >/dev/null
      git worktree add "$worktree" "$branch" >/dev/null
    else
      if [[ "$activity" == "fix-ci" ]]; then
        echo "No existing branch '$branch' for CI repair." >&2
        exit 1
      fi
      git worktree add -b "$branch" "$worktree" origin/main >/dev/null
    fi
  fi

  prompt="$(sed "s/{{ISSUE}}/$issue/g" "$prompt_path")"
fi

echo "CDDM Codex worker"
echo "  activity:  $activity"
echo "  target:    $target"
echo "  model:     $model"
echo "  reasoning: $reasoning"
echo "  worktree:  $worktree"

printf '%s\n' "$prompt" | codex exec \
  -C "$worktree" \
  --sandbox workspace-write \
  -m "$model" \
  -c "model_reasoning_effort=\"$reasoning\"" \
  -
