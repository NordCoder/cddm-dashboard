#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
observer="$repo_root/scripts/cddm-codex-observe.py"
change="$repo_root/scripts/cddm-codex-change.sh"
worker_shim="$repo_root/scripts/cddm-codex-worker-shim.sh"
host_tool_shim="$repo_root/scripts/cddm-host-tool-shim.sh"
issue=990032
runtime="$repo_root/.worktrees/runtime"
results="$repo_root/.worktrees/results"
state="$runtime/issue-$issue.json"
history="$runtime/issue-$issue-turns.jsonl"
events="$results/issue-$issue-resume-test.jsonl"
v2="$results/issue-$issue-v2-test.log"
lock="$runtime/issue-$issue.lock"
tmp="$(mktemp -d)"
mkdir -p "$runtime" "$results"

cleanup() {
  rm -f "$state" "$history" "$events" "$v2" "$lock"
  rm -rf "$tmp"
  if [[ -d "$repo_root/.worktrees/host-bin" ]]; then
    rm -f "$repo_root/.worktrees/host-bin/go" "$repo_root/.worktrees/host-bin/gofmt" \
      "$repo_root/.worktrees/host-bin/npm" "$repo_root/.worktrees/host-bin/docker" \
      "$repo_root/.worktrees/host-bin/tee" "$repo_root/.worktrees/host-bin/codex"
  fi
}
trap cleanup EXIT

cat >"$events" <<'EOF'
{"type":"thread.started","thread_id":"thread-r1"}
{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}
{"type":"item.completed","item":{"type":"command_execution","command":"go test ./...","exit_code":0}}
{"type":"future.event","payload":{"unbounded":"must-not-be-rendered"}}
not-json
EOF
cat >"$state" <<EOF
{"version":4,"issue":$issue,"status":"RUNNING","thread_id":"thread-r1","thread_generation":1,"thread_turn_count":1,"total_turn_count":1,"model":"gpt-test","reasoning":"medium","candidate_head":null,"pr":null,"active_mode":"resume","active_pid":$$,"active_events":"$events","active_result":"$results/issue-$issue-resume-test.result.json","active_v2_log":"$v2","updated_at":"2026-07-27T00:00:00Z"}
EOF
cat >"$history" <<EOF
{"turn_key":"$results/issue-$issue-resume-test.result.json","phase":"start","started_at":"2026-07-27T00:00:00Z","mode":"resume","model":"gpt-test","reasoning":"medium","events":"$events","result":"$results/issue-$issue-resume-test.result.json","v2_log":"$v2"}
{"turn_key":"$results/issue-$issue-resume-test.result.json","phase":"finish","ended_at":"2026-07-27T00:00:02Z","rc":0,"result_status":"CONTINUE","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":2}}
EOF

python3 -m py_compile "$observer"
bash -n "$change"
bash -n "$worker_shim"
bash -n "$host_tool_shim"

echo "[R1] pretty event rendering"
python3 "$observer" logs --repo "$repo_root" --issue "$issue" >"$tmp/logs.out"
grep -q 'THREAD  thread-r1' "$tmp/logs.out"
grep -q 'RUN     go test ./...' "$tmp/logs.out"
grep -q 'DONE    go test ./... · exit=0' "$tmp/logs.out"
grep -q 'EVENT   future.event' "$tmp/logs.out"
grep -q 'EVENT   malformed JSON' "$tmp/logs.out"
! grep -q 'unbounded' "$tmp/logs.out"

echo "[R1] status/logs/turns bypass mutating lock"
flock "$lock" -c 'sleep 2' &
locker=$!
sleep 0.1
timeout 1 "$change" status "$issue" >"$tmp/status.out"
timeout 1 "$change" logs "$issue" --raw >"$tmp/raw.out"
timeout 1 "$change" turns "$issue" >"$tmp/turns.out"
wait "$locker"
grep -q "Issue #$issue · RUNNING" "$tmp/status.out"
grep -q '10/4/2' "$tmp/turns.out"
grep -q 'thread.started' "$tmp/raw.out"

echo "[R1] watch exits independently when active state ends"
(
  sleep 0.5
  printf '%s\n' '{"type":"turn.completed"}' >>"$events"
  python3 - "$state" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
s = json.loads(p.read_text())
s["status"] = "CONTINUE"
s["active_mode"] = None
s["active_pid"] = None
p.write_text(json.dumps(s))
PY
) &
timeout 3 "$change" watch "$issue" --stall-seconds 1 >"$tmp/watch.out"
grep -q 'TURN    COMPLETED' "$tmp/watch.out"
grep -q 'active turn ended' "$tmp/watch.out"

# Restore active state for proxy/history tests.
python3 - "$state" <<PY
import json, pathlib
p = pathlib.Path("$state")
s = json.loads(p.read_text())
s.update({"status":"RUNNING","active_mode":"resume","active_pid":None,"active_events":"$events","active_result":"$results/issue-$issue-proxy.result.json","active_v2_log":"$v2","active_model":"gpt-test","active_reasoning":"medium"})
p.write_text(json.dumps(s))
PY
cat >"$results/issue-$issue-proxy.result.json" <<'EOF'
{"status":"CONTINUE","summary":"ok","verify":"ok","blocker":"none"}
EOF

echo "[R1] live proxy preserves raw JSONL and derives pretty activity"
python3 "$observer" proxy --repo "$repo_root" --issue "$issue" --mode resume --stall-seconds 5 -- \
  python3 -c 'import sys; print("{\"type\":\"thread.started\",\"thread_id\":\"proxy-thread\"}", flush=True); print("{\"type\":\"item.started\",\"item\":{\"type\":\"file_change\",\"path\":\"x.go\"}}", flush=True)' \
  >"$tmp/proxy.raw" 2>"$tmp/proxy.pretty"
grep -q 'proxy-thread' "$tmp/proxy.raw"
grep -q 'THREAD  proxy-thread' "$tmp/proxy.pretty"
grep -q 'EDIT    x.go' "$tmp/proxy.pretty"
grep -q 'CODEX   exit=0 · CONTINUE' "$tmp/proxy.pretty"

echo "[R1] stall warning is observational only"
python3 "$observer" proxy --repo "$repo_root" --issue "$issue" --mode resume --stall-seconds 1 -- \
  python3 -c 'import time; time.sleep(2); print("{\"type\":\"turn.completed\"}", flush=True)' \
  >"$tmp/stall.raw" 2>"$tmp/stall.pretty"
grep -q 'WARN    no Codex events' "$tmp/stall.pretty"
grep -q 'CODEX   exit=0' "$tmp/stall.pretty"

echo "[R1] recovery-only Codex guard never invokes executable"
CDDM_BLOCK_CODEX=1 "$worker_shim" login status
set +e
CDDM_BLOCK_CODEX=1 "$worker_shim" exec --json >/dev/null 2>&1
blocked_rc=$?
set -e
[[ $blocked_rc -eq 97 ]]

echo "[R1] stop refuses unrelated reused pid"
sleep 10 &
unrelated=$!
python3 - "$state" "$unrelated" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
s = json.loads(p.read_text())
s["active_mode"] = "resume"
s["active_pid"] = int(sys.argv[2])
p.write_text(json.dumps(s))
PY
set +e
"$change" stop "$issue" >"$tmp/stop.out" 2>"$tmp/stop.err"
stop_rc=$?
set -e
[[ $stop_rc -ne 0 ]]
kill -0 "$unrelated"
kill "$unrelated"
wait "$unrelated" 2>/dev/null || true
grep -q 'Refusing stop' "$tmp/stop.err"

echo "[R1] V2 presentation keeps full raw log and bounded terminal output"
fake="$tmp/fake-go"
cat >"$fake" <<'EOF'
#!/usr/bin/env bash
echo 'raw-v2-output'
exit "${FAKE_RC:-0}"
EOF
chmod +x "$fake"
ln -s "$host_tool_shim" "$tmp/go"
set +e
CDDM_HOST_V2_UI=1 CDDM_REPO_ROOT="$repo_root" CDDM_REAL_GO="$fake" "$tmp/go" test ./... 2>&1 \
  | python3 "$observer" v2-tee --log "$v2" >"$tmp/v2-pass.out"
pipe_rc=${PIPESTATUS[0]}
set -e
[[ $pipe_rc -eq 0 ]]
grep -q 'Go tests.*PASS' "$tmp/v2-pass.out"
grep -q 'raw-v2-output' "$v2"
! grep -q 'raw-v2-output' "$tmp/v2-pass.out"

set +e
FAKE_RC=7 CDDM_HOST_V2_UI=1 CDDM_REPO_ROOT="$repo_root" CDDM_REAL_GO="$fake" "$tmp/go" test ./... 2>&1 \
  | python3 "$observer" v2-tee --log "$v2" >"$tmp/v2-fail.out"
pipe_rc=${PIPESTATUS[0]}
set -e
[[ $pipe_rc -eq 7 ]]
grep -q 'Go tests.*FAIL' "$tmp/v2-fail.out"
grep -q 'raw-v2-output' "$tmp/v2-fail.out"

echo "Runtime R1 harness: PASS"
