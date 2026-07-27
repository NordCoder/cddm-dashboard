package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type workerResult struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Verify  string `json:"verify"`
	Blocker string `json:"blocker"`
}

func (e *engine) start(model, reasoning string) int {
	if err := e.preflight(true, true); err != nil {
		e.ui.errorf("preflight: %v", err)
		return 1
	}
	if err := e.repairPrethreadFailure(); err != nil {
		e.ui.errorf("pre-thread recovery: %v", err)
		return 1
	}
	if err := e.alignCleanPrethreadOrphan(); err != nil {
		e.ui.errorf("orphan reconciliation: %v", err)
		return 1
	}

	state, err := loadState(e.statePath)
	if err != nil && !os.IsNotExist(err) {
		e.ui.errorf("load runtime state: %v", err)
		return 1
	}
	if os.IsNotExist(err) {
		state = e.initialState(model, reasoning, "WORKTREE_INITIALIZING")
		if saveErr := saveStateAtomic(e.statePath, state); saveErr != nil {
			e.ui.errorf("initialize state: %v", saveErr)
			return 1
		}
		if err := e.createOrAttachWorktree(); err != nil {
			e.ui.errorf("create worktree: %v", err)
			return 1
		}
		state.Status = "INITIALIZING"
		if err := saveStateAtomic(e.statePath, state); err != nil {
			e.ui.errorf("persist state: %v", err)
			return 1
		}
	} else {
		if state.Contract != "" {
			e.contract = state.Contract
		}
		recovered, rc := e.recoverActive(&state)
		if recovered || rc != 0 {
			return rc
		}
		if state.ThreadID != "" {
			e.ui.errorf("persistent thread already exists for Issue #%d; use resume/status", e.issue)
			return 1
		}
		if err := e.createOrAttachWorktree(); err != nil {
			e.ui.errorf("adopt worktree: %v", err)
			return 1
		}
		state.Model = model
		state.Reasoning = reasoning
		state.Status = "INITIALIZING"
		if err := saveStateAtomic(e.statePath, state); err != nil {
			e.ui.errorf("persist state: %v", err)
			return 1
		}
	}

	ctx, err := e.issueContext()
	if err != nil {
		e.ui.errorf("read Issue #%d: %v", e.issue, err)
		return 1
	}
	prompt, err := e.renderTemplate("change-start.md", ctx, "")
	if err != nil {
		e.ui.errorf("render start prompt: %v", err)
		return 1
	}
	return e.runTurn("start", "", model, reasoning, prompt, "")
}

func (e *engine) resumeOrRotate(mode, instruction string, overrides []string) int {
	if err := e.preflight(true, true); err != nil {
		e.ui.errorf("preflight: %v", err)
		return 1
	}
	state, err := loadState(e.statePath)
	if err != nil {
		e.ui.errorf("no persistent session state for Issue #%d; use start", e.issue)
		return 1
	}
	if state.Contract != "" {
		e.contract = state.Contract
	}
	if err := e.validateWorktree(false); err != nil {
		e.ui.errorf("worktree validation: %v", err)
		return 1
	}
	if refExists(e.repo, "refs/remotes/origin/"+e.branch) {
		local, localErr := runOutput(e.worktree, nil, "git", "rev-parse", "HEAD")
		remote, remoteErr := runOutput(e.repo, nil, "git", "rev-parse", "origin/"+e.branch)
		if localErr != nil || remoteErr != nil || strings.TrimSpace(local) != strings.TrimSpace(remote) {
			e.ui.errorf("remote Change branch moved outside this session")
			return 1
		}
	}
	recovered, rc := e.recoverActive(&state)
	if recovered || rc != 0 {
		return rc
	}
	if state.ThreadID == "" {
		e.ui.errorf("persistent thread_id missing")
		return 1
	}
	model := state.Model
	reasoning := state.Reasoning
	if len(overrides) >= 1 {
		model = overrides[0]
	}
	if len(overrides) >= 2 {
		reasoning = overrides[1]
	}
	state.Model = model
	state.Reasoning = reasoning
	if err := saveStateAtomic(e.statePath, state); err != nil {
		e.ui.errorf("persist execution settings: %v", err)
		return 1
	}

	template := "change-resume.md"
	if mode == "rotate" {
		template = "change-rotate.md"
	}
	prompt, err := e.renderTemplate(template, "", instruction)
	if err != nil {
		e.ui.errorf("render %s prompt: %v", mode, err)
		return 1
	}
	return e.runTurn(mode, state.ThreadID, model, reasoning, prompt, instruction)
}

func (e *engine) initialState(model, reasoning, status string) RuntimeState {
	return RuntimeState{
		Version:          4,
		Issue:            e.issue,
		Branch:           e.branch,
		Worktree:         e.worktree,
		Model:            model,
		Reasoning:        reasoning,
		Contract:         e.contract,
		Status:           status,
		ThreadGeneration: 1,
		ThreadHistory:    []ThreadHistoryEntry{},
	}
}

func (e *engine) runTurn(mode, previousThread, model, reasoning, prompt, rotationReason string) int {
	state, err := loadState(e.statePath)
	if err != nil {
		e.ui.errorf("load state: %v", err)
		return 1
	}
	if err := e.prepareWorkerRuntime(); err != nil {
		e.ui.errorf("prepare isolated Worker runtime: %v", err)
		return 1
	}
	if err := os.MkdirAll(e.resultsDir, 0o755); err != nil {
		e.ui.errorf("create results directory: %v", err)
		return 1
	}
	stamp := fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid())
	events := filepath.Join(e.resultsDir, fmt.Sprintf("issue-%d-%s-%s.jsonl", e.issue, mode, stamp))
	result := filepath.Join(e.resultsDir, fmt.Sprintf("issue-%d-%s-%s.result.json", e.issue, mode, stamp))
	v2Log := filepath.Join(e.resultsDir, fmt.Sprintf("issue-%d-v2-%s.log", e.issue, stamp))
	pidFile := filepath.Join(e.resultsDir, fmt.Sprintf("issue-%d-%s-%s.pid", e.issue, mode, stamp))
	exitStatus := filepath.Join(e.resultsDir, fmt.Sprintf("issue-%d-%s-%s.exit-status", e.issue, mode, stamp))
	f, err := os.OpenFile(events, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		e.ui.errorf("create event log: %v", err)
		return 1
	}
	defer f.Close()
	_ = os.Remove(pidFile)
	_ = os.Remove(exitStatus)

	state.ActivePID = nil
	state.ActivePIDFile = pidFile
	state.ActivePIDStartTicks = ""
	state.ActivePGID = nil
	state.ActiveMode = mode
	state.ActiveEvents = events
	state.ActiveResult = result
	state.ActiveV2Log = v2Log
	state.ActiveExitStatus = exitStatus
	state.ActivePreviousThread = previousThread
	state.ActiveRotationReason = rotationReason
	state.ActiveModel = model
	state.ActiveReasoning = reasoning
	state.Status = "RUNNING"
	if err := saveStateAtomic(e.statePath, state); err != nil {
		e.ui.errorf("persist active turn intent: %v", err)
		return 1
	}

	cmd, err := e.codexCommand(mode, previousThread, model, reasoning, result)
	if err != nil {
		e.ui.errorf("prepare Codex command: %v", err)
		return 1
	}
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		e.ui.errorf("prepare Codex stdout: %v", err)
		return 1
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		e.ui.errorf("start Codex: %v", err)
		return 1
	}
	identity, identityErr := readProcessIdentity(cmd.Process.Pid)
	if identityErr != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
		e.ui.errorf("capture Codex process identity: %v", identityErr)
		return 1
	}
	state.ActivePID = ptr(identity.PID)
	state.ActivePGID = ptr(identity.PGID)
	state.ActivePIDStartTicks = identity.StartTicks
	if err := writeIntAtomic(pidFile, identity.PID); err != nil {
		_ = syscall.Kill(-identity.PGID, syscall.SIGTERM)
		e.ui.errorf("persist Codex pid: %v", err)
		return 1
	}
	if err := saveStateAtomic(e.statePath, state); err != nil {
		_ = syscall.Kill(-identity.PGID, syscall.SIGTERM)
		e.ui.errorf("persist active process identity: %v", err)
		return 1
	}

	_ = appendHistory(e.historyPath, map[string]any{
		"turn_key": result, "phase": "start", "started_at": utcNow(), "mode": mode,
		"model": model, "reasoning": reasoning, "events": events, "result": result,
		"v2_log": v2Log, "exit_status": exitStatus,
	})
	e.ui.header(os.Stderr, e.issue, fmt.Sprintf("%s · %s / %s", mode, model, reasoning))

	type lineResult struct {
		line string
		err  error
	}
	lines := make(chan lineResult, 64)
	go func() {
		defer close(lines)
		r := bufio.NewReaderSize(stdout, 128*1024)
		for {
			line, readErr := r.ReadString('\n')
			if line != "" {
				lines <- lineResult{line: line}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					lines <- lineResult{err: readErr}
				}
				return
			}
		}
	}()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastEvent := time.Now()
	lastWarn := time.Time{}
	startTime := time.Now()
	var usage Usage
	var childErr error
	childDone := false
	linesDone := false
	threadSeen := ""
	threadInvalid := false

	for !(childDone && linesDone) {
		select {
		case lr, ok := <-lines:
			if !ok {
				linesDone = true
				continue
			}
			if lr.err != nil {
				e.ui.warnf(os.Stderr, "event stream read failed: %v", lr.err)
				continue
			}
			_, _ = io.WriteString(f, lr.line)
			_ = f.Sync()
			trimmed := strings.TrimSuffix(lr.line, "\n")
			var event map[string]any
			if json.Unmarshal([]byte(trimmed), &event) == nil {
				parseUsage(event, &usage)
				if eventThread := threadFromEvent(event); eventThread != "" && threadSeen == "" {
					threadSeen = eventThread
					if err := e.bindLiveThread(&state, mode, previousThread, eventThread, model, reasoning, rotationReason); err != nil {
						threadInvalid = true
						e.ui.errorf("thread identity: %v", err)
						_ = syscall.Kill(-identity.PGID, syscall.SIGTERM)
					}
				}
			}
			e.ui.printEvent(os.Stderr, time.Since(startTime), parseRenderedEvent(trimmed))
			lastEvent = time.Now()
			lastWarn = time.Time{}
		case err := <-waitCh:
			childErr = err
			childDone = true
		case sig := <-sigCh:
			_ = syscall.Kill(-identity.PGID, sig.(syscall.Signal))
		case <-ticker.C:
			stall := envStallSeconds()
			if stall > 0 && time.Since(lastEvent) >= time.Duration(stall)*time.Second && (lastWarn.IsZero() || time.Since(lastWarn) >= time.Duration(stall)*time.Second) {
				e.ui.warnf(os.Stderr, "no Codex events for %s · process still alive · observation only", humanDuration(time.Since(lastEvent)))
				lastWarn = time.Now()
			}
		}
	}
	_ = f.Sync()
	rc := exitCode(childErr)
	if err := writeIntAtomic(exitStatus, rc); err != nil {
		state.Status = "TURN_COMPLETION_UNKNOWN"
		_ = saveStateAtomic(e.statePath, state)
		e.ui.errorf("persist durable Codex completion status: %v", err)
		return 15
	}
	_ = os.Remove(pidFile)
	if threadInvalid {
		state.Status = "THREAD_MISMATCH"
		_ = saveStateAtomic(e.statePath, state)
		return 11
	}
	if threadSeen == "" {
		switch mode {
		case "start":
			state.Status = "START_FAILED_NO_THREAD"
		case "rotate":
			state.Status = "ROTATE_FAILED_NO_THREAD"
		default:
			state.Status = "THREAD_MISMATCH"
		}
		_ = saveStateAtomic(e.statePath, state)
		e.ui.errorf("completed %s turn has no valid thread.started identity", mode)
		return 10
	}
	e.countTurn(&state, result)
	_ = appendHistory(e.historyPath, map[string]any{
		"turn_key": result, "phase": "finish", "ended_at": utcNow(), "rc": rc,
		"result_status": resultStatus(result), "usage": usage,
	})

	if rc != 0 {
		state.Status = "TURN_FAILED"
		state.LastResult = result
		state.LastResultRC = ptr(rc)
		e.clearActive(&state)
		_ = saveStateAtomic(e.statePath, state)
		e.ui.errorf("Codex turn failed; worktree/session preserved (rc=%d)", rc)
		return rc
	}
	if info, statErr := os.Stat(result); statErr != nil || info.Size() == 0 {
		state.Status = "EMPTY_RESULT"
		state.LastResult = result
		state.LastResultRC = ptr(12)
		e.clearActive(&state)
		_ = saveStateAtomic(e.statePath, state)
		e.ui.errorf("Codex produced no final result")
		return 12
	}
	if _, err := validateWorkerResult(result); err != nil {
		state.Status = "INVALID_RESULT"
		state.LastResult = result
		state.LastResultRC = ptr(13)
		e.clearActive(&state)
		_ = saveStateAtomic(e.statePath, state)
		e.ui.errorf("invalid structured result: %v", err)
		return 13
	}

	dispatchRC := e.dispatchResult(&state, result, v2Log)
	if state.LastResult == result {
		e.clearActive(&state)
		_ = saveStateAtomic(e.statePath, state)
	}
	return dispatchRC
}

func (e *engine) prepareWorkerRuntime() error {
	if err := e.ensureWorkerHome(); err != nil {
		return err
	}
	workerCodexHome := filepath.Join(e.workerHome, ".codex")
	if err := os.MkdirAll(filepath.Join(workerCodexHome, "sqlite"), 0o700); err != nil {
		return err
	}
	configSrc := filepath.Join(e.worktree, ".codex", "config.toml")
	configDst := filepath.Join(workerCodexHome, "config.toml")
	if err := copyFile(configSrc, configDst, 0o600); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(workerCodexHome, "auth.json")); os.IsNotExist(err) {
		hostHome := os.Getenv("CODEX_HOME")
		if hostHome == "" {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return homeErr
			}
			hostHome = filepath.Join(home, ".codex")
		}
		if err := copyFile(filepath.Join(hostHome, "auth.json"), filepath.Join(workerCodexHome, "auth.json"), 0o600); err != nil {
			return fmt.Errorf("host Codex auth.json unavailable: %w", err)
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func (e *engine) codexCommand(mode, previousThread, model, reasoning, result string) (*exec.Cmd, error) {
	codex, err := exec.LookPath("codex")
	if err != nil {
		return nil, err
	}
	schema := filepath.Join(e.repo, ".codex", "schemas", "change-turn-result.json")
	if _, err := os.Stat(schema); err != nil {
		return nil, fmt.Errorf("missing result schema: %s", schema)
	}
	args := []string{
		"exec", "--strict-config", "--json", "-C", e.worktree, "-m", model,
		"-c", fmt.Sprintf("model_reasoning_effort=%q", reasoning),
		"-c", fmt.Sprintf("default_permissions=%q", permissionProfile),
		"--output-schema", schema, "--output-last-message", result,
	}
	if mode == "resume" {
		args = append(args, "resume", previousThread, "-")
	} else {
		args = append(args, "-")
	}
	cmd := exec.Command(codex, args...)
	cmd.Dir = e.worktree
	cmd.Env = e.workerEnv()
	return cmd, nil
}

func (e *engine) workerEnv() []string {
	workerCodexHome := filepath.Join(e.workerHome, ".codex")
	blocked := map[string]bool{
		"GH_TOKEN": true, "GITHUB_TOKEN": true, "GITHUB_ENTERPRISE_TOKEN": true,
		"SSH_AUTH_SOCK": true, "GIT_ASKPASS": true, "SSH_ASKPASS": true,
	}
	var env []string
	for _, item := range os.Environ() {
		key := item
		if idx := strings.IndexByte(item, '='); idx >= 0 {
			key = item[:idx]
		}
		if blocked[key] || key == "HOME" || key == "XDG_CONFIG_HOME" || key == "XDG_CACHE_HOME" || key == "XDG_DATA_HOME" || key == "GH_CONFIG_DIR" || key == "GIT_CONFIG_GLOBAL" || key == "CODEX_HOME" || key == "CODEX_SQLITE_HOME" {
			continue
		}
		env = append(env, item)
	}
	env = append(env,
		"HOME="+e.workerHome,
		"XDG_CONFIG_HOME="+filepath.Join(e.workerHome, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(e.workerHome, ".cache"),
		"XDG_DATA_HOME="+filepath.Join(e.workerHome, ".local", "share"),
		"GH_CONFIG_DIR="+filepath.Join(e.workerHome, ".config", "gh"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"CODEX_HOME="+workerCodexHome,
		"CODEX_SQLITE_HOME="+filepath.Join(workerCodexHome, "sqlite"),
	)
	return env
}

func (e *engine) bindLiveThread(state *RuntimeState, mode, previous, found, model, reasoning, rotationReason string) error {
	switch mode {
	case "start":
		if state.ThreadID == "" {
			state.ThreadID = found
			state.Status = "RUNNING"
			return saveStateAtomic(e.statePath, *state)
		}
		if state.ThreadID != found {
			return fmt.Errorf("start returned %s, persisted thread is %s", found, state.ThreadID)
		}
	case "resume":
		if previous == "" || found != previous || state.ThreadID != previous {
			return fmt.Errorf("resume returned %s, expected persistent thread %s", found, previous)
		}
	case "rotate":
		if previous == "" || found == previous {
			return errors.New("rotate did not establish a fresh thread")
		}
		if state.ThreadID == previous {
			state.ThreadHistory = append(state.ThreadHistory, ThreadHistoryEntry{
				ThreadID: state.ThreadID, Model: state.Model, Reasoning: state.Reasoning,
				TurnCount: state.ThreadTurnCount, RotatedAt: utcNow(), Reason: rotationReason,
			})
			state.ThreadID = found
			state.Model = model
			state.Reasoning = reasoning
			state.ThreadTurnCount = 0
			state.ThreadGeneration++
			state.Status = "ROTATED"
			return saveStateAtomic(e.statePath, *state)
		}
		if state.ThreadID != found {
			return fmt.Errorf("rotate returned %s, persisted thread is %s", found, state.ThreadID)
		}
	default:
		return fmt.Errorf("unknown turn mode %q", mode)
	}
	return nil
}

func threadFromEvent(event map[string]any) string {
	if stringValue(event["type"]) != "thread.started" {
		return ""
	}
	if id := stringValue(event["thread_id"]); id != "" {
		return id
	}
	if thread, ok := event["thread"].(map[string]any); ok {
		return stringValue(thread["id"])
	}
	return ""
}

func threadFromEvents(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		var event map[string]any
		if json.Unmarshal(s.Bytes(), &event) == nil {
			if id := threadFromEvent(event); id != "" {
				return id
			}
		}
	}
	return ""
}

func validateWorkerResult(path string) (workerResult, error) {
	var result workerResult
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return result, err
	}
	if len(raw) != 4 {
		return result, errors.New("result must contain exactly status, summary, verify, blocker")
	}
	for _, key := range []string{"status", "summary", "verify", "blocker"} {
		if _, ok := raw[key]; !ok {
			return result, fmt.Errorf("missing result field %s", key)
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	switch result.Status {
	case "CANDIDATE_READY", "CONTINUE", "BLOCKED", "NO_OP":
	default:
		return result, fmt.Errorf("invalid result status %q", result.Status)
	}
	if strings.TrimSpace(result.Summary) == "" || strings.TrimSpace(result.Verify) == "" || strings.TrimSpace(result.Blocker) == "" {
		return result, errors.New("summary, verify and blocker must be non-empty strings")
	}
	return result, nil
}

func resultStatus(path string) string {
	result, err := validateWorkerResult(path)
	if err != nil {
		return ""
	}
	return result.Status
}

func (e *engine) countTurn(state *RuntimeState, result string) {
	if state.LastCountedResult == result {
		return
	}
	state.ThreadTurnCount++
	state.TotalTurnCount++
	state.LastCountedResult = result
}

func (e *engine) clearActive(state *RuntimeState) {
	state.ActivePID = nil
	state.ActivePIDFile = ""
	state.ActivePIDStartTicks = ""
	state.ActivePGID = nil
	state.ActiveMode = ""
	state.ActiveEvents = ""
	state.ActiveResult = ""
	state.ActiveV2Log = ""
	state.ActiveExitStatus = ""
	state.ActivePreviousThread = ""
	state.ActiveRotationReason = ""
	state.ActiveModel = ""
	state.ActiveReasoning = ""
}
