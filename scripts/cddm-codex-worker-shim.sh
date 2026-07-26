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
  set +e
  CODEX_HOME="$worker_codex_home" CODEX_SQLITE_HOME="$worker_codex_home/sqlite" \
    python3 "$observer" proxy --repo "$repo_root" --issue "$issue" --mode "$mode" -- \
      "$real_codex" "${filtered[@]}"
  proxy_rc=$?
  set -e

  # Python Popen reports a child killed by a signal as a negative return code;
  # SystemExit(-15/-2) reaches the shell as 241/254. Normalize the two Host
  # control signals back to the canonical shell convention so the core writes
  # durable 143/130 completion evidence.
  normalized_rc="$proxy_rc"
  case "$proxy_rc" in
    241) normalized_rc=143 ;;
    254) normalized_rc=130 ;;
  esac

  if [[ "$normalized_rc" != "$proxy_rc" ]]; then
    python3 - "$repo_root" "$issue" "$normalized_rc" <<'PY' || true
import datetime as dt
import fcntl
import json
import pathlib
import sys

repo = pathlib.Path(sys.argv[1])
issue = int(sys.argv[2])
rc = int(sys.argv[3])
state_path = repo / ".worktrees" / "runtime" / f"issue-{issue}.json"
history_path = repo / ".worktrees" / "runtime" / f"issue-{issue}-turns.jsonl"
try:
    state = json.loads(state_path.read_text())
except Exception:
    raise SystemExit(0)
turn_key = state.get("active_result") or state.get("active_events")
if not turn_key:
    raise SystemExit(0)
row = {
    "turn_key": turn_key,
    "phase": "finish",
    "ended_at": dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "rc": rc,
    "signal_normalized": True,
}
history_path.parent.mkdir(parents=True, exist_ok=True)
with history_path.open("a", encoding="utf-8") as handle:
    fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
    handle.write(json.dumps(row, separators=(",", ":")) + "\n")
    handle.flush()
    fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
PY
  fi
  exit "$normalized_rc"
fi

CODEX_HOME="$worker_codex_home" CODEX_SQLITE_HOME="$worker_codex_home/sqlite" exec "$real_codex" "${filtered[@]}"
