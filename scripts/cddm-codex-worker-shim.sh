#!/usr/bin/env bash
set -euo pipefail

# `recover` must be able to enter the legacy core without invoking the real
# Codex executable. The core still probes `codex login status`; satisfy only
# that non-execution probe and reject every other Codex path.
if [[ "${CDDM_BLOCK_CODEX:-0}" == "1" ]]; then
  if [[ "${1:-}" == "login" && "${2:-}" == "status" ]]; then
    exit 0
  fi
  echo "Refusing Codex invocation in recovery-only mode." >&2
  exit 97
fi

real_codex="${CDDM_REAL_CODEX:-}"
[[ -n "$real_codex" && -x "$real_codex" ]] || { echo "CDDM Codex shim has no real Codex executable." >&2; exit 1; }

if [[ "${1:-}" != "exec" ]]; then
  exec "$real_codex" "$@"
fi

host_codex_home="${CODEX_HOME:-}"
[[ -n "$host_codex_home" ]] || { echo "CDDM Codex shim requires the host CODEX_HOME." >&2; exit 1; }

args=("$@")
worktree=""
for ((i=0; i<${#args[@]}; i++)); do
  if [[ "${args[$i]}" == "-C" ]]; then
    (( i + 1 < ${#args[@]} )) || { echo "Codex -C is missing its path." >&2; exit 1; }
    worktree="${args[$((i+1))]}"
    break
  fi
done
[[ -n "$worktree" && -f "$worktree/.codex/config.toml" ]] || { echo "Unable to resolve controlled Change config from Codex -C worktree." >&2; exit 1; }

worker_codex_home="$HOME/.codex"
mkdir -p "$worker_codex_home" "$worker_codex_home/sqlite"
cp "$worktree/.codex/config.toml" "$worker_codex_home/config.toml"
chmod 600 "$worker_codex_home/config.toml"

if [[ ! -s "$worker_codex_home/auth.json" ]]; then
  [[ -s "$host_codex_home/auth.json" ]] || { echo "Host Codex auth.json is unavailable for isolated Worker runtime." >&2; exit 1; }
  install -m 600 "$host_codex_home/auth.json" "$worker_codex_home/auth.json"
fi

filtered=()
has_json=0
for arg in "${args[@]}"; do
  [[ "$arg" == "--ignore-user-config" ]] && continue
  [[ "$arg" == "--json" ]] && has_json=1
  filtered+=("$arg")
done

# Host V2 presentation shims must be transparent inside Codex/code-mode.
unset CDDM_HOST_V2_UI

repo_root="${CDDM_REPO_ROOT:-}"
issue="${CDDM_RUNTIME_ISSUE:-}"
mode="${CDDM_RUNTIME_MODE:-unknown}"
observer="${repo_root:+$repo_root/scripts/cddm-codex-observe.py}"

if [[ $has_json -eq 1 && -n "$repo_root" && "$issue" =~ ^[0-9]+$ && -f "$observer" ]]; then
  CODEX_HOME="$worker_codex_home" CODEX_SQLITE_HOME="$worker_codex_home/sqlite" \
    exec python3 "$observer" proxy --repo "$repo_root" --issue "$issue" --mode "$mode" -- \
      "$real_codex" "${filtered[@]}"
fi

CODEX_HOME="$worker_codex_home" CODEX_SQLITE_HOME="$worker_codex_home/sqlite" exec "$real_codex" "${filtered[@]}"
