#!/usr/bin/env bash
set -euo pipefail

tool="$(basename "$0")"
runtime_bin="${CDDM_CHANGE_BIN:-}"

real_tool() {
  case "$tool" in
    go) printf '%s' "${CDDM_REAL_GO:-}" ;;
    gofmt) printf '%s' "${CDDM_REAL_GOFMT:-}" ;;
    npm) printf '%s' "${CDDM_REAL_NPM:-}" ;;
    docker) printf '%s' "${CDDM_REAL_DOCKER:-}" ;;
    tee) printf '%s' "${CDDM_REAL_TEE:-}" ;;
    *) return 1 ;;
  esac
}

real="$(real_tool)"
[[ -n "$real" && -x "$real" ]] || { echo "CDDM Host tool shim cannot resolve real '$tool'." >&2; exit 127; }

# Worker/Codex subprocesses explicitly unset this flag. Nested tools launched
# by a top-level V2 tool inherit depth=1 and remain transparent.
if [[ "${CDDM_HOST_V2_UI:-0}" != "1" || "${CDDM_V2_TOOL_DEPTH:-0}" != "0" ]]; then
  exec "$real" "$@"
fi

if [[ "$tool" == "tee" ]]; then
  if [[ $# -eq 1 && "$1" == *"-v2-"*".log" && -n "$runtime_bin" && -x "$runtime_bin" ]]; then
    exec "$runtime_bin" __v2tee --log "$1"
  fi
  exec "$real" "$@"
fi

phase="$tool"
case "$tool:$*" in
  "gofmt:"*) phase="gofmt" ;;
  "go:test -race "*|"go:test -race"*) phase="Go race" ;;
  "go:test "*|"go:test"*) phase="Go tests" ;;
  "npm:ci"*) phase="npm install" ;;
  "npm:test"*) phase="npm test" ;;
  "npm:run build"*) phase="npm build" ;;
  "docker:compose config"*) phase="Docker Compose config" ;;
esac

start_epoch="$(date +%s)"
printf '@@CDDM_V2@@|START|%s|%s\n' "$phase" "$start_epoch" >&2

if [[ "$tool" == "gofmt" ]]; then
  set +e
  output="$(CDDM_V2_TOOL_DEPTH=1 "$real" "$@")"
  rc=$?
  set -e
  [[ -z "$output" ]] || printf '%s\n' "$output"
  semantic_rc="$rc"
  if [[ $semantic_rc -eq 0 && -n "$output" && " $* " == *" -l "* ]]; then
    semantic_rc=1
  fi
  end_epoch="$(date +%s)"
  duration=$(( end_epoch - start_epoch ))
  printf '@@CDDM_V2@@|END|%s|%s|%s|\n' "$phase" "$semantic_rc" "$duration" >&2
  exit "$rc"
fi

set +e
CDDM_V2_TOOL_DEPTH=1 "$real" "$@"
rc=$?
set -e
end_epoch="$(date +%s)"
duration=$(( end_epoch - start_epoch ))
printf '@@CDDM_V2@@|END|%s|%s|%s|\n' "$phase" "$rc" "$duration" >&2
exit "$rc"
