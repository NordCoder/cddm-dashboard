# Change — Runtime R1 Observability and Recovery

Issue: #32  
Type: PROCESS / Trusted Host runtime  
Risk: High  
Authorized Base: `5d5af57401372dfb41e1c6304670801d756ca34f`

## Outcome

Make long-running WebLead 3.0 Codex Changes continuously observable and safely operable without altering Worker authority, Candidate semantics, or M6 product behavior.

A normal foreground `start`, `resume`, or `rotate` shows bounded human-readable Codex activity and Host V2 progress. A second terminal can inspect or watch the Change without blocking the mutating Host command. Dead/interrupted turns can be recovered explicitly without a fake resume instruction, and an active owned turn can be stopped without killing an unrelated reused PID.

## Out of Scope

- M6 backend/browser/frontend product behavior;
- Chrome extension execution;
- reconcile / PR-number preservation redesign (Runtime R2);
- automatic inactivity timeout, kill, rotation, or retry;
- daemon/service architecture;
- changing Worker sandbox permissions;
- changing V2/V3/Candidate authority.

## HARD HOW

### 1. Event evidence and derived presentation

`codex exec --json` raw JSONL remains the canonical turn event artifact.

- One raw event file is created per Codex turn under `.worktrees/results/`.
- Pretty output is a derived view only; it must never replace, rewrite, filter, or become required to consume the raw artifact.
- Rendering failure is non-consequential: the Codex process, durable result, exit status, thread reconciliation, V2, and Candidate flow must continue independently.
- Unknown/new Codex event shapes degrade to a bounded generic display line or are safely omitted. They never fail the turn.
- Human-readable output may summarize agent/reasoning/message text, command activity, file changes, errors, completion, and usage evidence, but must bound line/payload size and must not emit prompt files, environment secrets, auth material, or raw unbounded payloads.

### 2. Foreground live activity

While a mutating Codex turn is active, the foreground Host invocation continuously renders newly appended JSONL events.

The operator must be able to distinguish at minimum:

- Host/Codex turn start and mode;
- thread identity once known;
- command/tool execution start and completion when represented by Codex events;
- file-change/edit activity when represented;
- bounded agent/reasoning/message summaries when represented;
- errors;
- turn completion/result transition;
- elapsed turn time;
- age of the most recently observed event.

The activity renderer must not spawn a second Codex operation or consume the Worker result.

### 3. Read-only observer boundary

The commands below are observational and MUST NOT acquire the per-Issue mutating `flock`:

```text
status <issue>
status <issue> --json
watch <issue>
logs <issue>
logs <issue> --raw
logs <issue> --v2
turns <issue>
```

They:

- never invoke Codex;
- never perform Git/GitHub publication or branch mutation;
- never mutate runtime state merely because it is viewed;
- remain usable while `start`/`resume`/`rotate`/`recover`/`stop` owns the Issue mutation lock;
- tolerate a state file being atomically replaced while being read;
- exit independently on viewer `Ctrl+C` without signalling the active Host/Codex session.

`status <issue>` becomes concise human-readable output by default. `status <issue> --json` preserves raw-state compatibility.

### 4. Durable per-turn history

R1 adds durable per-turn observation history to the runtime state rather than reconstructing history from filenames.

Each turn record has a stable Host-generated turn identity and snapshots at minimum:

- mode (`start|resume|rotate`);
- model and reasoning;
- thread generation / expected thread identity when known;
- UTC start time;
- completion time when known;
- duration when derivable;
- raw event path;
- result path;
- V2 log path;
- durable exit-status path/value when known;
- Worker structured result status when valid;
- observed usage/token/cache fields only when Codex emitted them.

Crash consistency:

- the turn record is persisted before spawning the Codex child;
- normal completion, recovery, and failure finalize the same turn record idempotently;
- recovery must never append a duplicate history record for the same turn;
- historical records are not rewritten to imply success after a failure/unknown outcome;
- legacy state files without turn history are migrated/read compatibly and do not block existing Changes.

Exact internal JSON field names/schema version are implementation freedom, but the semantics above are fixed.

### 5. Stall visibility is observational only

The Host/watch view reports the age of the most recently observed event while the owned process is alive.

- Default warning threshold: 60 seconds unless an existing runtime configuration mechanism provides a better bounded setting.
- Warnings may repeat at a bounded interval.
- Event inactivity is never proof of a hung model.
- No inactivity threshold may automatically kill, rotate, retry, or fail a turn in R1.

### 6. Explicit `recover`

Add:

```text
scripts/cddm-codex-change.sh recover <issue>
```

Hard guarantee: `recover` never invokes Codex.

It runs the Host-owned durable recovery state machine directly and under the per-Issue mutation lock.

Required semantics:

- if no active/unreconciled turn exists, report a successful no-op;
- if the recorded owned process is still alive, refuse recovery and report it as active;
- if durable exit/result evidence proves a completed turn, reconcile thread/result exactly once using the existing authority rules;
- a durable non-zero exit such as 130/143 becomes failed-turn state while preserving thread/worktree and clearing active fields once safely consumed;
- missing/inconsistent durable evidence remains fail-closed (`TURN_COMPLETION_UNKNOWN`/equivalent) and is not guessed;
- repeated `recover` after successful reconciliation is idempotent;
- recovery does not require or accept a fake Worker instruction.

Existing implicit recovery before `resume` may remain for compatibility, but explicit recovery is the normal operator path.

### 7. Explicit `stop`

Add:

```text
scripts/cddm-codex-change.sh stop <issue>
```

`stop` is a Host mutation and acquires the per-Issue mutation lock only long enough/where necessary to serialize state-changing stop/recovery operations.

It may signal only a process session proven to belong to the persisted active turn.

Proof must combine persisted turn identity with live process identity strongly enough to reject PID reuse/unrelated processes. Persist additional host/session identity in active state if required.

Required semantics:

- no active turn -> idempotent no-op;
- stale/reused/unproven PID/session -> refuse to signal and report fail-closed;
- valid active owned Host/Codex session -> terminate the complete owned session/descendants, preserving worktree/thread;
- never target the calling observer/controlling shell session;
- after termination, reconcile durable completion when available; otherwise leave a recoverable explicit state rather than fabricating a result;
- never launch a replacement Codex turn.

### 8. Readable Host V2

V2 retains the complete raw log artifact and exact existing verification authority.

Render explicit phases with PASS/FAIL and duration for:

1. gofmt;
2. `go test ./...`;
3. `go test -race ./...`;
4. `npm ci`;
5. `npm test`;
6. `npm run build`;
7. `docker compose config --quiet`.

On failure:

- identify the exact failed phase;
- show only bounded useful tail/context in the terminal;
- retain the complete raw V2 log;
- publish no Candidate, exactly as today.

Pretty V2 output must not change command ordering or pass/fail semantics.

### 9. Usage telemetry

Usage telemetry is best-effort observation only.

- Parse token/cache usage only from Codex event fields actually emitted.
- Missing, malformed, or future event shapes yield unknown fields and never fail a turn.
- `turns` may display observed input/output/cached-token counts.
- Runtime must not infer or claim ChatGPT weekly quota percentage, billing cost, or unobserved token usage.

### 10. Compatibility and ownership

- One Change still owns one persistent Codex thread until explicit rotation.
- Current raw result schema and Worker statuses remain unchanged.
- Current process-session isolation and Ctrl+C process-tree termination remain authoritative.
- Foreground mutating-command `Ctrl+C` still terminates the owned Host/Codex session; observer `Ctrl+C` only exits the observer.
- Existing start/resume/rotate CLI remains accepted.
- Existing runtime state files from pre-R1 Changes remain inspectable/recoverable.
- No browser-delivery Product semantics are modified.

## Required CLI behavior

Representative UX, exact cosmetics implementation freedom:

```text
$ ./scripts/cddm-codex-change.sh status 32
Issue #32 · RUNNING
mode        resume
model       gpt-5.6-luna / medium
thread      019f...
elapsed     04:17
last event  3s ago
process     alive
worktree    3 modified, 1 untracked
candidate   none
```

```text
$ ./scripts/cddm-codex-change.sh turns 32
#  MODE    MODEL          DURATION  RC   RESULT
1  start   Luna/medium    05:12     0    CONTINUE
2  resume  Luna/medium    03:41     0    CANDIDATE_READY
```

`--json` / `--raw` modes expose machine evidence rather than pretty output.

## Implementation Freedom

Worker may choose:

- shell vs a narrow Python helper for JSONL rendering/history summaries;
- exact pretty formatting and ANSI/color detection;
- polling interval and event-offset bookkeeping;
- exact state schema version/field names;
- helper decomposition and runtime harness organization.

Worker may NOT:

- make rendering a dependency of Codex completion;
- make read-only commands acquire the mutating Issue lock;
- add automatic inactivity cancellation;
- weaken PID/session ownership checks;
- alter Worker permissions or Candidate/V2/V3 authority;
- implement R2 reconcile/PR changes.

## Verification

Required focused evidence:

1. recorded/fake JSONL is rendered incrementally; unknown and bounded malformed lines do not terminate producer/turn;
2. raw JSONL remains byte-preserved while pretty output is produced;
3. `status/watch/logs/turns` execute while a separate process holds the Issue mutation lock;
4. observer `Ctrl+C` does not signal an active fake Host turn;
5. inactivity warning appears after threshold but fake Codex remains alive;
6. a dead turn with durable rc=143 is recovered by `recover` without invoking fake Codex and preserves thread/worktree;
7. repeated `recover` is idempotent;
8. `stop` rejects a reused/unrelated PID and does not signal it;
9. `stop` terminates a proven owned fake process session including descendants, then leaves/reconciles a deterministic state;
10. turn history is created before spawn and finalized exactly once on normal completion and recovery;
11. legacy runtime state without R1 history remains compatible;
12. V2 pretty phases report an injected failing phase and Candidate publication remains absent;
13. absent/unknown usage telemetry is tolerated;
14. `bash -n` / helper tests / deterministic runtime harness pass;
15. repository V2 and exact-Head CI pass;
16. fresh Web Lead QA.

## Dependencies

Built on current Trusted Host runtime including process-session isolation, interrupted-turn recovery, PID identity hardening, and `reconcile` support. Product #17/#18 are already merged, but R1 must not depend on or modify their product behavior.

Owner review: not required.
