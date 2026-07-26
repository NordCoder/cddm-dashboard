#!/usr/bin/env python3
"""Fail-open presentation proxy for `codex exec --json`.

The child Codex process is the consequential operation. Everything this proxy
adds (pretty rendering, turn history, token telemetry, stall warnings) is
best-effort. Once the child has started, observer failures degrade to raw
stdout pass-through and can never intentionally terminate/cancel the child.
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import os
import pathlib
import selectors
import signal
import subprocess
import sys
import time
from typing import Any


def load_observer(repo: pathlib.Path) -> Any | None:
    path = repo / "scripts" / "cddm-codex-observe.py"
    try:
        spec = importlib.util.spec_from_file_location("cddm_codex_observe", path)
        if spec is None or spec.loader is None:
            return None
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module
    except Exception as exc:
        print(f"WARN    observer unavailable ({type(exc).__name__}); raw Codex stream remains active", file=sys.stderr, flush=True)
        return None


def safe_call(module: Any | None, name: str, *args: Any, **kwargs: Any) -> Any | None:
    if module is None:
        return None
    try:
        return getattr(module, name)(*args, **kwargs)
    except Exception as exc:
        print(f"WARN    observer {name} failed ({type(exc).__name__}); continuing Codex turn", file=sys.stderr, flush=True)
        return None


def normalize_rc(rc: int) -> int:
    return 128 + (-rc) if rc < 0 else rc


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True)
    parser.add_argument("--issue", required=True, type=int)
    parser.add_argument("--mode", default="unknown")
    parser.add_argument("--stall-seconds", type=int, default=int(os.environ.get("CDDM_STALL_SECONDS", "60")))
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = list(args.command)
    if command and command[0] == "--":
        command = command[1:]
    if not command:
        parser.error("command required after --")

    repo = pathlib.Path(args.repo).resolve()

    # Start the consequential child before doing any telemetry I/O. If this
    # Popen itself fails, there is no Codex turn to preserve and surfacing the
    # launch failure is correct.
    proc = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=None, text=True, bufsize=1)
    assert proc.stdout is not None

    observer = load_observer(repo)
    state: dict[str, Any] = {}
    history_file: pathlib.Path | None = None
    turn_key: str | None = None
    result_path: str | None = None
    usage: dict[str, int | None] = {"input_tokens": None, "cached_input_tokens": None, "output_tokens": None}

    if observer is not None:
        try:
            state_file, history_file, _ = observer.repo_paths(repo, args.issue)
            state = observer.load_json(state_file)
            turn_key = observer.turn_key_from_state(state)
            result_value = state.get("active_result")
            result_path = result_value if isinstance(result_value, str) and result_value else None
            safe_call(
                observer,
                "append_history",
                history_file,
                {
                    "turn_key": turn_key,
                    "phase": "start",
                    "started_at": observer.utc_now(),
                    "mode": state.get("active_mode") or args.mode,
                    "model": state.get("active_model") or state.get("model"),
                    "reasoning": state.get("active_reasoning") or state.get("reasoning"),
                    "events": state.get("active_events"),
                    "result": result_value,
                    "v2_log": state.get("active_v2_log"),
                    "exit_status": state.get("active_exit_status"),
                },
            )
            print(
                f"CDDM    Issue #{args.issue} · {state.get('active_mode') or args.mode} · "
                f"{state.get('active_model') or state.get('model') or 'model?'} / "
                f"{state.get('active_reasoning') or state.get('reasoning') or 'reasoning?'}",
                file=sys.stderr,
                flush=True,
            )
        except Exception as exc:
            print(f"WARN    observer initialization failed ({type(exc).__name__}); raw Codex stream remains active", file=sys.stderr, flush=True)
            observer = None

    received_signal: int | None = None

    def forward_signal(signum: int, _frame: Any) -> None:
        nonlocal received_signal
        received_signal = signum
        try:
            proc.send_signal(signum)
        except ProcessLookupError:
            pass

    old_int = signal.signal(signal.SIGINT, forward_signal)
    old_term = signal.signal(signal.SIGTERM, forward_signal)
    last_event = time.monotonic()
    warned_at = 0.0
    selector = selectors.DefaultSelector()
    selector.register(proc.stdout, selectors.EVENT_READ)

    try:
        while True:
            try:
                ready = selector.select(timeout=1.0)
            except Exception as exc:
                # Presentation timing failed. Fall back to a blocking raw drain;
                # do not signal the child.
                print(f"WARN    observer polling failed ({type(exc).__name__}); raw pass-through enabled", file=sys.stderr, flush=True)
                for line in proc.stdout:
                    sys.stdout.write(line)
                    sys.stdout.flush()
                break

            if ready:
                try:
                    line = proc.stdout.readline()
                except Exception as exc:
                    print(f"WARN    observer read failed ({type(exc).__name__}); waiting for Codex completion", file=sys.stderr, flush=True)
                    line = ""
                if line:
                    sys.stdout.write(line)
                    sys.stdout.flush()
                    last_event = time.monotonic()
                    warned_at = 0.0
                    if observer is not None:
                        try:
                            event = json.loads(line)
                            if isinstance(event, dict):
                                observed = observer.parse_usage_event(event)
                                observer.merge_usage(usage, observed)
                        except Exception:
                            pass
                        rendered = safe_call(observer, "render_line", line)
                        if isinstance(rendered, str):
                            print(rendered, file=sys.stderr, flush=True)
                    continue

            child_rc = proc.poll()
            if child_rc is not None:
                # Drain any final buffered lines. Rendering remains optional.
                try:
                    for line in proc.stdout:
                        sys.stdout.write(line)
                        sys.stdout.flush()
                        if observer is not None:
                            rendered = safe_call(observer, "render_line", line)
                            if isinstance(rendered, str):
                                print(rendered, file=sys.stderr, flush=True)
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

        child_rc = proc.wait()
    finally:
        signal.signal(signal.SIGINT, old_int)
        signal.signal(signal.SIGTERM, old_term)
        selector.close()

    rc = normalize_rc(child_rc)
    if received_signal is not None and rc == 0:
        rc = 128 + received_signal

    result_status: Any = None
    if observer is not None and result_path:
        result = safe_call(observer, "load_json", pathlib.Path(result_path))
        if isinstance(result, dict):
            result_status = result.get("status")
    if observer is not None and history_file is not None and turn_key:
        safe_call(
            observer,
            "append_history",
            history_file,
            {
                "turn_key": turn_key,
                "phase": "finish",
                "ended_at": safe_call(observer, "utc_now") or None,
                "rc": rc,
                "result_status": result_status,
                "usage": usage,
            },
        )
    print(f"CODEX   exit={rc}" + (f" · {result_status}" if result_status else ""), file=sys.stderr, flush=True)
    return rc


if __name__ == "__main__":
    raise SystemExit(main())
