#!/usr/bin/env python3
"""Read-only/runtime presentation helpers for the CDDM Codex Host.

Raw Codex JSONL and Host state remain authoritative. This module only derives
bounded operator output and append-only turn telemetry. It intentionally uses
only the Python standard library.
"""

from __future__ import annotations

import argparse
import datetime as dt
import fcntl
import json
import os
import pathlib
import re
import selectors
import signal
import subprocess
import sys
import time
from collections import deque
from typing import Any, Iterable

MAX_TEXT = 360
DEFAULT_STALL_SECONDS = int(os.environ.get("CDDM_STALL_SECONDS", "60"))
SECRET_PATTERNS = [
    re.compile(r"(?i)(authorization\s*:\s*)([^\s]+)"),
    re.compile(r"(?i)((?:password|passwd|secret|token|cookie)\s*[=:]\s*)([^\s,;]+)"),
    re.compile(r"\bgh[pousr]_[A-Za-z0-9_]{20,}\b"),
    re.compile(r"\bsk-[A-Za-z0-9_-]{16,}\b"),
]


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def parse_time(value: Any) -> dt.datetime | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None


def human_duration(seconds: float | int | None) -> str:
    if seconds is None or seconds < 0:
        return "unknown"
    seconds = int(seconds)
    hours, rem = divmod(seconds, 3600)
    minutes, secs = divmod(rem, 60)
    if hours:
        return f"{hours:d}:{minutes:02d}:{secs:02d}"
    return f"{minutes:02d}:{secs:02d}"


def redact(text: str) -> str:
    value = text.replace("\x00", "").replace("\r", " ").replace("\n", " ")
    for pattern in SECRET_PATTERNS:
        if pattern.groups >= 2:
            value = pattern.sub(r"\1[REDACTED]", value)
        else:
            value = pattern.sub("[REDACTED]", value)
    value = re.sub(r"\s+", " ", value).strip()
    if len(value) > MAX_TEXT:
        value = value[: MAX_TEXT - 1] + "…"
    return value


def repo_paths(repo: pathlib.Path, issue: int) -> tuple[pathlib.Path, pathlib.Path, pathlib.Path]:
    runtime = repo / ".worktrees" / "runtime"
    results = repo / ".worktrees" / "results"
    return runtime / f"issue-{issue}.json", runtime / f"issue-{issue}-turns.jsonl", results


def load_json(path: pathlib.Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text())
        return value if isinstance(value, dict) else {}
    except (OSError, json.JSONDecodeError):
        return {}


def read_history(path: pathlib.Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    try:
        with path.open() as handle:
            for line in handle:
                try:
                    value = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(value, dict):
                    rows.append(value)
    except OSError:
        pass
    return rows


def append_history(path: pathlib.Path, row: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    encoded = json.dumps(row, ensure_ascii=False, separators=(",", ":")) + "\n"
    with path.open("a", encoding="utf-8") as handle:
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
        handle.write(encoded)
        handle.flush()
        os.fsync(handle.fileno())
        fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


def turn_key_from_state(state: dict[str, Any]) -> str:
    for key in ("active_result", "last_result", "active_events"):
        value = state.get(key)
        if isinstance(value, str) and value:
            return value
    return f"unknown:{utc_now()}"


def fold_history(rows: Iterable[dict[str, Any]]) -> list[dict[str, Any]]:
    order: list[str] = []
    folded: dict[str, dict[str, Any]] = {}
    for row in rows:
        key = str(row.get("turn_key") or "")
        if not key:
            continue
        if key not in folded:
            folded[key] = {"turn_key": key}
            order.append(key)
        folded[key].update(row)
    return [folded[key] for key in order]


def find_latest_file(results: pathlib.Path, issue: int, suffix: str) -> pathlib.Path | None:
    try:
        candidates = [p for p in results.glob(f"issue-{issue}-*{suffix}") if p.is_file()]
        return max(candidates, key=lambda p: p.stat().st_mtime) if candidates else None
    except OSError:
        return None


def latest_paths(repo: pathlib.Path, issue: int) -> tuple[dict[str, Any], pathlib.Path | None, pathlib.Path | None]:
    state_file, history_file, results = repo_paths(repo, issue)
    state = load_json(state_file)
    events: pathlib.Path | None = None
    v2: pathlib.Path | None = None
    active_events = state.get("active_events")
    active_v2 = state.get("active_v2_log")
    if isinstance(active_events, str) and active_events:
        events = pathlib.Path(active_events)
    if isinstance(active_v2, str) and active_v2:
        v2 = pathlib.Path(active_v2)
    if events is None or not events.exists() or v2 is None or not v2.exists():
        turns = fold_history(read_history(history_file))
        for turn in reversed(turns):
            if events is None or not events.exists():
                candidate = turn.get("events")
                if isinstance(candidate, str) and pathlib.Path(candidate).exists():
                    events = pathlib.Path(candidate)
            if v2 is None or not v2.exists():
                candidate = turn.get("v2_log")
                if isinstance(candidate, str) and pathlib.Path(candidate).exists():
                    v2 = pathlib.Path(candidate)
            if events is not None and events.exists() and v2 is not None and v2.exists():
                break
    if events is None or not events.exists():
        events = find_latest_file(results, issue, ".jsonl")
    if v2 is None or not v2.exists():
        v2 = find_latest_file(results, issue, ".log")
    return state, events, v2


def item_text(item: dict[str, Any]) -> str:
    for key in ("text", "message", "summary", "reasoning", "content"):
        value = item.get(key)
        if isinstance(value, str) and value.strip():
            return redact(value)
    return ""


def command_value(item: dict[str, Any]) -> str:
    for key in ("command", "cmd"):
        value = item.get(key)
        if isinstance(value, str):
            return redact(value)
        if isinstance(value, list):
            return redact(" ".join(str(part) for part in value))
    return ""


def file_change_summary(item: dict[str, Any]) -> str:
    changes = item.get("changes")
    paths: list[str] = []
    if isinstance(changes, list):
        for change in changes[:8]:
            if isinstance(change, dict):
                path = change.get("path") or change.get("file")
                kind = change.get("kind") or change.get("type")
                if path:
                    paths.append(f"{kind or 'edit'} {path}")
    for key in ("path", "file"):
        if not paths and isinstance(item.get(key), str):
            paths.append(str(item[key]))
    return redact(", ".join(paths)) if paths else "file changes"


def render_event(event: dict[str, Any]) -> str | None:
    event_type = str(event.get("type") or event.get("event") or "unknown")
    if event_type == "thread.started":
        thread = event.get("thread_id")
        if not thread and isinstance(event.get("thread"), dict):
            thread = event["thread"].get("id")
        return f"THREAD  {redact(str(thread or 'started'))}"
    if event_type in {"turn.started", "turn.completed", "turn.failed", "turn.cancelled"}:
        label = event_type.split(".", 1)[1].upper()
        detail = event.get("message") or event.get("error") or ""
        return f"TURN    {label}" + (f" · {redact(str(detail))}" if detail else "")
    if event_type == "error" or event_type.endswith(".error"):
        detail = event.get("message") or event.get("error") or event.get("detail") or "error"
        return f"ERROR   {redact(str(detail))}"

    item = event.get("item") if isinstance(event.get("item"), dict) else event
    item_type = str(item.get("type") or event.get("item_type") or "")
    phase = event_type.split(".")[-1]
    if item_type in {"agent_message", "message"}:
        text = item_text(item)
        return f"MESSAGE {text}" if text else f"MESSAGE {phase}"
    if item_type in {"reasoning", "analysis"}:
        text = item_text(item)
        return f"THINK   {text}" if text else f"THINK   {phase}"
    if item_type in {"command_execution", "command", "shell_command"}:
        command = command_value(item)
        if phase in {"started", "created"}:
            return f"RUN     {command or 'command'}"
        exit_code = item.get("exit_code")
        status = item.get("status")
        result = f"exit={exit_code}" if exit_code is not None else str(status or phase)
        return f"DONE    {command + ' · ' if command else ''}{redact(result)}"
    if item_type in {"file_change", "file_changes", "patch", "edit"}:
        return f"EDIT    {file_change_summary(item)}"
    if item_type in {"mcp_tool_call", "tool_call"}:
        name = item.get("tool") or item.get("name") or item.get("server") or "tool"
        return f"TOOL    {redact(str(name))} · {phase}"
    if item_type in {"web_search", "search"}:
        query = item.get("query") or "search"
        return f"SEARCH  {redact(str(query))} · {phase}"
    if item_type in {"todo_list", "plan"}:
        return f"PLAN    {phase}"
    if event_type.startswith("item."):
        return f"ITEM    {redact(item_type or 'unknown')} · {phase}"
    # Unknown event payloads are deliberately not dumped: raw JSONL remains on disk.
    return f"EVENT   {redact(event_type)}"


def parse_usage_event(event: dict[str, Any]) -> dict[str, int | None]:
    result: dict[str, int | None] = {"input_tokens": None, "cached_input_tokens": None, "output_tokens": None}
    candidates: list[dict[str, Any]] = [event]
    for key in ("usage", "token_usage", "tokens"):
        value = event.get(key)
        if isinstance(value, dict):
            candidates.append(value)
    for candidate in candidates:
        mapping = {
            "input_tokens": ("input_tokens", "input", "prompt_tokens"),
            "cached_input_tokens": ("cached_input_tokens", "cached_tokens", "cache_read_tokens"),
            "output_tokens": ("output_tokens", "output", "completion_tokens"),
        }
        for target, names in mapping.items():
            for name in names:
                value = candidate.get(name)
                if isinstance(value, int) and value >= 0:
                    current = result[target]
                    result[target] = max(current or 0, value)
                    break
    return result


def merge_usage(total: dict[str, int | None], observed: dict[str, int | None]) -> None:
    for key, value in observed.items():
        if value is not None:
            current = total.get(key)
            total[key] = max(current or 0, value)


def usage_from_file(path: pathlib.Path | None) -> dict[str, int | None]:
    total: dict[str, int | None] = {"input_tokens": None, "cached_input_tokens": None, "output_tokens": None}
    if path is None:
        return total
    try:
        for line in path.read_text(errors="replace").splitlines():
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            if isinstance(event, dict):
                merge_usage(total, parse_usage_event(event))
    except OSError:
        pass
    return total


def render_line(line: str) -> str:
    try:
        event = json.loads(line)
    except json.JSONDecodeError:
        return "EVENT   malformed JSON (raw line preserved)"
    if not isinstance(event, dict):
        return "EVENT   non-object JSON (raw line preserved)"
    return render_event(event) or "EVENT   unrendered"


def cmd_proxy(args: argparse.Namespace) -> int:
    repo = pathlib.Path(args.repo).resolve()
    issue = args.issue
    state_file, history_file, _ = repo_paths(repo, issue)
    state = load_json(state_file)
    turn_key = turn_key_from_state(state)
    started_at = utc_now()
    start_row = {
        "turn_key": turn_key,
        "phase": "start",
        "started_at": started_at,
        "mode": state.get("active_mode") or args.mode,
        "model": state.get("active_model") or state.get("model"),
        "reasoning": state.get("active_reasoning") or state.get("reasoning"),
        "events": state.get("active_events"),
        "result": state.get("active_result"),
        "v2_log": state.get("active_v2_log"),
        "exit_status": state.get("active_exit_status"),
    }
    append_history(history_file, start_row)
    print(
        f"CDDM    Issue #{issue} · {start_row.get('mode') or args.mode} · "
        f"{start_row.get('model') or 'model?'} / {start_row.get('reasoning') or 'reasoning?'}",
        file=sys.stderr,
        flush=True,
    )

    proc = subprocess.Popen(args.command, stdout=subprocess.PIPE, stderr=None, text=True, bufsize=1)
    assert proc.stdout is not None
    selector = selectors.DefaultSelector()
    selector.register(proc.stdout, selectors.EVENT_READ)
    last_event = time.monotonic()
    warned_at = 0.0
    usage: dict[str, int | None] = {"input_tokens": None, "cached_input_tokens": None, "output_tokens": None}
    received_signal: int | None = None

    def handle_signal(signum: int, _frame: Any) -> None:
        nonlocal received_signal
        received_signal = signum
        try:
            proc.send_signal(signum)
        except ProcessLookupError:
            pass

    old_int = signal.signal(signal.SIGINT, handle_signal)
    old_term = signal.signal(signal.SIGTERM, handle_signal)
    try:
        while True:
            ready = selector.select(timeout=1.0)
            if ready:
                line = proc.stdout.readline()
                if line:
                    sys.stdout.write(line)
                    sys.stdout.flush()
                    last_event = time.monotonic()
                    try:
                        event = json.loads(line)
                    except json.JSONDecodeError:
                        event = None
                    if isinstance(event, dict):
                        merge_usage(usage, parse_usage_event(event))
                    try:
                        rendered = render_line(line)
                        print(rendered, file=sys.stderr, flush=True)
                    except Exception as exc:  # presentation must never abort the producer
                        print(f"EVENT   renderer disabled for line: {type(exc).__name__}", file=sys.stderr, flush=True)
                    warned_at = 0.0
                    continue
            rc = proc.poll()
            if rc is not None:
                # Drain any buffered final JSONL lines.
                for line in proc.stdout:
                    sys.stdout.write(line)
                    sys.stdout.flush()
                    try:
                        event = json.loads(line)
                    except json.JSONDecodeError:
                        event = None
                    if isinstance(event, dict):
                        merge_usage(usage, parse_usage_event(event))
                    try:
                        print(render_line(line), file=sys.stderr, flush=True)
                    except Exception:
                        pass
                break
            idle = time.monotonic() - last_event
            if args.stall_seconds > 0 and idle >= args.stall_seconds:
                if warned_at == 0.0 or time.monotonic() - warned_at >= args.stall_seconds:
                    print(
                        f"WARN    no Codex events for {int(idle)}s · process still alive · observation only",
                        file=sys.stderr,
                        flush=True,
                    )
                    warned_at = time.monotonic()
        rc = proc.wait()
    finally:
        signal.signal(signal.SIGINT, old_int)
        signal.signal(signal.SIGTERM, old_term)
        selector.close()

    if received_signal is not None and rc >= 0:
        rc = 128 + received_signal
    result_status = None
    result_path = start_row.get("result")
    if isinstance(result_path, str) and result_path:
        result = load_json(pathlib.Path(result_path))
        result_status = result.get("status")
    append_history(
        history_file,
        {
            "turn_key": turn_key,
            "phase": "finish",
            "ended_at": utc_now(),
            "rc": rc,
            "result_status": result_status,
            "usage": usage,
        },
    )
    print(f"CODEX   exit={rc}" + (f" · {result_status}" if result_status else ""), file=sys.stderr, flush=True)
    return rc


def active_started_at(history_file: pathlib.Path, state: dict[str, Any]) -> dt.datetime | None:
    active_result = state.get("active_result")
    turns = fold_history(read_history(history_file))
    for turn in reversed(turns):
        if active_result and turn.get("result") == active_result:
            return parse_time(turn.get("started_at"))
    return None


def process_alive(pid: Any) -> bool:
    if not isinstance(pid, int) or pid <= 0:
        return False
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def cmd_status(args: argparse.Namespace) -> int:
    repo = pathlib.Path(args.repo).resolve()
    state_file, history_file, _ = repo_paths(repo, args.issue)
    if not state_file.exists():
        print(f"No runtime state for Issue #{args.issue}.")
        return 0
    if args.json:
        sys.stdout.write(state_file.read_text())
        if not state_file.read_text().endswith("\n"):
            print()
        return 0
    state = load_json(state_file)
    print(f"Issue #{args.issue} · {state.get('status') or 'UNKNOWN'}")
    print()
    print("Thread")
    print(f"  id          {state.get('thread_id') or '-'}")
    print(f"  generation  {state.get('thread_generation', 1)}")
    print(f"  turns       {state.get('thread_turn_count', 0)} current / {state.get('total_turn_count', 0)} total")
    print(f"  model       {state.get('model') or '-'} / {state.get('reasoning') or '-'}")

    active_mode = state.get("active_mode")
    if active_mode:
        started = active_started_at(history_file, state)
        elapsed = (dt.datetime.now(dt.timezone.utc) - started).total_seconds() if started else None
        events = pathlib.Path(str(state.get("active_events") or ""))
        age: float | None = None
        try:
            age = time.time() - events.stat().st_mtime if events.exists() else None
        except OSError:
            pass
        pid = state.get("active_pid")
        print()
        print("Active")
        print(f"  mode        {active_mode}")
        print(f"  elapsed     {human_duration(elapsed)}")
        print(f"  last event  {human_duration(age)} ago" if age is not None else "  last event  unknown")
        print(f"  process     {'alive' if process_alive(pid) else 'not observed'}" + (f" (pid={pid})" if pid else ""))

    print()
    print("Candidate")
    print(f"  head        {state.get('candidate_head') or '-'}")
    print(f"  PR          #{state.get('pr')}" if state.get("pr") else "  PR          -")

    worktree = state.get("worktree")
    if isinstance(worktree, str) and worktree and pathlib.Path(worktree).is_dir():
        try:
            out = subprocess.run(
                ["git", "-C", worktree, "status", "--short"],
                check=False,
                capture_output=True,
                text=True,
                timeout=5,
            ).stdout.splitlines()
            print()
            print(f"Worktree    {len(out)} changed path(s)")
            for line in out[:12]:
                print(f"  {line}")
            if len(out) > 12:
                print(f"  … {len(out) - 12} more")
        except (OSError, subprocess.SubprocessError):
            pass
    return 0


def iter_event_lines(path: pathlib.Path) -> Iterable[str]:
    try:
        with path.open(errors="replace") as handle:
            yield from handle
    except OSError:
        return


def cmd_logs(args: argparse.Namespace) -> int:
    repo = pathlib.Path(args.repo).resolve()
    _, events, v2 = latest_paths(repo, args.issue)
    if args.v2:
        if v2 is None or not v2.exists():
            print(f"No V2 log found for Issue #{args.issue}.")
            return 0
        sys.stdout.write(v2.read_text(errors="replace"))
        return 0
    if events is None or not events.exists():
        print(f"No Codex event log found for Issue #{args.issue}.")
        return 0
    if args.raw:
        sys.stdout.write(events.read_text(errors="replace"))
        return 0
    print(f"Codex events · Issue #{args.issue} · {events}")
    for line in iter_event_lines(events):
        print(render_line(line))
    return 0


def cmd_watch(args: argparse.Namespace) -> int:
    repo = pathlib.Path(args.repo).resolve()
    state_file, history_file, _ = repo_paths(repo, args.issue)
    if not state_file.exists():
        print(f"No runtime state for Issue #{args.issue}.")
        return 0
    print(f"Watching Issue #{args.issue} · Ctrl+C exits observer only")
    state = load_json(state_file)
    print(f"STATE   {state.get('status') or 'UNKNOWN'}")
    current_path: pathlib.Path | None = None
    offset = 0
    last_event = time.monotonic()
    warned_at = 0.0
    try:
        while True:
            state = load_json(state_file)
            active = bool(state.get("active_mode"))
            path_value = state.get("active_events")
            path = pathlib.Path(path_value) if isinstance(path_value, str) and path_value else None
            if path != current_path:
                current_path = path
                offset = 0
                last_event = time.monotonic()
                if current_path:
                    print(f"EVENTS  {current_path}")
            if current_path and current_path.exists():
                try:
                    with current_path.open(errors="replace") as handle:
                        handle.seek(offset)
                        while True:
                            line = handle.readline()
                            if not line:
                                break
                            offset = handle.tell()
                            last_event = time.monotonic()
                            warned_at = 0.0
                            print(render_line(line), flush=True)
                except OSError:
                    pass
            if not active:
                print(f"STATE   {state.get('status') or 'IDLE'} · active turn ended")
                return 0
            idle = time.monotonic() - last_event
            pid = state.get("active_pid")
            if args.stall_seconds > 0 and idle >= args.stall_seconds and process_alive(pid):
                if warned_at == 0.0 or time.monotonic() - warned_at >= args.stall_seconds:
                    print(f"WARN    no Codex events for {int(idle)}s · process still alive · observation only", flush=True)
                    warned_at = time.monotonic()
            time.sleep(0.5)
    except KeyboardInterrupt:
        print("\nObserver stopped; Host turn was not signalled.")
        return 130


def cmd_turns(args: argparse.Namespace) -> int:
    repo = pathlib.Path(args.repo).resolve()
    _, history_file, _ = repo_paths(repo, args.issue)
    turns = fold_history(read_history(history_file))
    if not turns:
        print(f"No R1 turn history recorded for Issue #{args.issue}. Legacy state remains readable via status/logs.")
        return 0
    print(f"Issue #{args.issue} turn history")
    print("#  MODE    MODEL / REASONING              DURATION  RC    RESULT             TOKENS in/cache/out")
    for index, turn in enumerate(turns[-args.limit :], start=max(1, len(turns) - args.limit + 1)):
        start = parse_time(turn.get("started_at"))
        end = parse_time(turn.get("ended_at"))
        duration = (end - start).total_seconds() if start and end else None
        usage = turn.get("usage") if isinstance(turn.get("usage"), dict) else {}
        tokens = "/".join(
            "?" if usage.get(key) is None else str(usage.get(key))
            for key in ("input_tokens", "cached_input_tokens", "output_tokens")
        )
        mode = str(turn.get("mode") or "?")
        model = str(turn.get("model") or "?")
        reasoning = str(turn.get("reasoning") or "?")
        rc = "-" if turn.get("rc") is None else str(turn.get("rc"))
        result = str(turn.get("result_status") or ("RUNNING" if end is None else "unknown"))
        print(f"{index:<2} {mode:<7} {(model + ' / ' + reasoning):<30} {human_duration(duration):>8}  {rc:<5} {result:<18} {tokens}")
    return 0


def cmd_record_recovery(args: argparse.Namespace) -> int:
    repo = pathlib.Path(args.repo).resolve()
    state_file, history_file, _ = repo_paths(repo, args.issue)
    state = load_json(state_file)
    rows = read_history(history_file)
    turns = fold_history(rows)
    last_result = state.get("last_result")
    if not isinstance(last_result, str) or not last_result:
        return 0
    target = None
    for turn in reversed(turns):
        if turn.get("result") == last_result or turn.get("turn_key") == last_result:
            target = turn
            break
    if target is None or target.get("ended_at"):
        return 0
    result = load_json(pathlib.Path(last_result))
    events = target.get("events")
    usage = usage_from_file(pathlib.Path(events) if isinstance(events, str) and events else None)
    append_history(
        history_file,
        {
            "turn_key": target["turn_key"],
            "phase": "finish",
            "ended_at": utc_now(),
            "rc": state.get("last_result_rc"),
            "result_status": result.get("status"),
            "usage": usage,
            "recovered": True,
        },
    )
    return 0


def cmd_v2_tee(args: argparse.Namespace) -> int:
    log = pathlib.Path(args.log)
    log.parent.mkdir(parents=True, exist_ok=True)
    current: str | None = None
    buffer: deque[str] = deque(maxlen=args.failure_lines)
    index = 0
    total = 7
    with log.open("w", encoding="utf-8") as handle:
        for line in sys.stdin:
            handle.write(line)
            handle.flush()
            if line.startswith("@@CDDM_V2@@|"):
                parts = line.rstrip("\n").split("|")
                if len(parts) >= 4 and parts[1] == "START":
                    current = parts[2]
                    index += 1
                    buffer.clear()
                    print(f"[{index}/{total}] {current:<24} RUN", flush=True)
                elif len(parts) >= 6 and parts[1] == "END":
                    phase = parts[2]
                    rc = parts[3]
                    duration = parts[4]
                    state = "PASS" if rc == "0" else "FAIL"
                    print(f"[{index}/{total}] {phase:<24} {state}  {duration}s", flush=True)
                    if rc != "0" and buffer:
                        print("--- failure context ---", flush=True)
                        for buffered in buffer:
                            print(buffered, flush=True)
                        print("--- full log preserved ---", flush=True)
                    current = None
                continue
            if current is not None:
                clean = line.rstrip("\n")
                if clean:
                    buffer.append(clean)
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command_name", required=True)

    def common(name: str) -> argparse.ArgumentParser:
        p = sub.add_parser(name)
        p.add_argument("--repo", required=True)
        p.add_argument("--issue", required=True, type=int)
        return p

    p = common("proxy")
    p.add_argument("--mode", default="unknown")
    p.add_argument("--stall-seconds", type=int, default=DEFAULT_STALL_SECONDS)
    p.add_argument("command", nargs=argparse.REMAINDER)

    p = common("status")
    p.add_argument("--json", action="store_true")

    p = common("logs")
    group = p.add_mutually_exclusive_group()
    group.add_argument("--raw", action="store_true")
    group.add_argument("--v2", action="store_true")

    p = common("watch")
    p.add_argument("--stall-seconds", type=int, default=DEFAULT_STALL_SECONDS)

    p = common("turns")
    p.add_argument("--limit", type=int, default=20)

    common("record-recovery")

    p = sub.add_parser("v2-tee")
    p.add_argument("--log", required=True)
    p.add_argument("--failure-lines", type=int, default=30)
    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    if args.command_name == "proxy":
        if args.command and args.command[0] == "--":
            args.command = args.command[1:]
        if not args.command:
            parser.error("proxy requires a command after --")
        return cmd_proxy(args)
    if args.command_name == "status":
        return cmd_status(args)
    if args.command_name == "logs":
        return cmd_logs(args)
    if args.command_name == "watch":
        return cmd_watch(args)
    if args.command_name == "turns":
        return cmd_turns(args)
    if args.command_name == "record-recovery":
        return cmd_record_recovery(args)
    if args.command_name == "v2-tee":
        return cmd_v2_tee(args)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
