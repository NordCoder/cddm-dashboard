package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

func (e *engine) recoverCommand() int {
	if err := e.preflight(false, false); err != nil {
		e.ui.errorf("recovery preflight: %v", err)
		return 1
	}
	state, err := loadState(e.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No runtime state for Issue #%d.\n", e.issue)
			return 0
		}
		e.ui.errorf("load runtime state: %v", err)
		return 1
	}
	before := state.ActiveMode != "" || state.ActivePID != nil
	recovered, rc := e.recoverActive(&state)
	if !before {
		fmt.Printf("Issue #%d has no active turn to recover.\n", e.issue)
		printStatusDashboard(e.ui, os.Stdout, e.issue, state, e.historyPath)
		return 0
	}
	if recovered && state.ActiveMode == "" && state.ActivePID == nil {
		fmt.Printf("Recovered Issue #%d active turn: status=%s", e.issue, state.Status)
		if state.LastResultRC != nil {
			fmt.Printf(", prior_rc=%d", *state.LastResultRC)
		}
		fmt.Println(".")
		printStatusDashboard(e.ui, os.Stdout, e.issue, state, e.historyPath)
		return 0
	}
	if rc == 0 {
		rc = 15
	}
	e.ui.errorf("recovery remains fail-closed: status=%s rc=%d", state.Status, rc)
	return rc
}

func (e *engine) recoverActive(state *RuntimeState) (bool, int) {
	if state.ActiveMode == "" && state.ActivePID == nil && state.ActiveEvents == "" && state.ActiveResult == "" {
		return false, 0
	}

	pid := 0
	if state.ActivePID != nil {
		pid = *state.ActivePID
	} else if state.ActivePIDFile != "" {
		if p, err := readIntFile(state.ActivePIDFile); err == nil {
			pid = p
		}
	}
	if pid > 0 && processExists(pid) {
		if _, owned := stateOwnsProcess(*state); owned {
			e.ui.errorf("a prior Codex turn is still active for Issue #%d (pid=%d)", e.issue, pid)
			return false, 3
		}
		// PID reuse must never prevent no-Codex recovery. A live but unowned PID is
		// not signal authority; only the durable turn evidence below may decide the
		// old turn's disposition.
		e.ui.warnf(os.Stderr, "persisted pid=%d is live but no longer matches the recorded Codex turn; continuing durable recovery without signalling it", pid)
	}

	if err := e.reconcileRecoveredThread(state); err != nil {
		state.Status = "THREAD_MISMATCH"
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("recovered thread identity: %v", err)
		return false, 4
	}
	if state.ActiveResult == "" {
		state.Status = "TURN_RESULT_IDENTITY_MISSING"
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("dead active turn has no durable result identity")
		return false, 15
	}
	if state.ActiveExitStatus == "" {
		state.Status = "TURN_COMPLETION_UNKNOWN"
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("dead active turn has no durable completion status identity")
		return false, 15
	}
	codexRC, err := readExitStatus(state.ActiveExitStatus)
	if err != nil {
		state.Status = "TURN_COMPLETION_UNKNOWN"
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("dead active turn has no valid durable Codex completion status")
		return false, 15
	}

	e.countTurn(state, state.ActiveResult)
	if codexRC != 0 {
		state.Status = "TURN_FAILED"
		state.LastResult = state.ActiveResult
		state.LastResultRC = ptr(codexRC)
		turnKey := state.ActiveResult
		e.clearActive(state)
		if err := saveStateAtomic(e.statePath, *state); err != nil {
			e.ui.errorf("persist recovered failure: %v", err)
			return false, 15
		}
		_ = appendHistory(e.historyPath, map[string]any{
			"turn_key": turnKey, "phase": "recovery", "recovered_at": utcNow(), "status": state.Status, "rc": codexRC,
		})
		e.ui.warnf(os.Stderr, "recovered Codex turn completed with rc=%d; result was not dispatched", codexRC)
		return true, codexRC
	}
	info, statErr := os.Stat(state.ActiveResult)
	if statErr != nil || info.Size() == 0 {
		state.Status = "EMPTY_RESULT"
		state.LastResult = state.ActiveResult
		state.LastResultRC = ptr(12)
		e.clearActive(state)
		_ = saveStateAtomic(e.statePath, *state)
		return true, 12
	}
	if _, err := validateWorkerResult(state.ActiveResult); err != nil {
		state.Status = "INVALID_RESULT"
		state.LastResult = state.ActiveResult
		state.LastResultRC = ptr(13)
		e.clearActive(state)
		_ = saveStateAtomic(e.statePath, *state)
		return true, 13
	}

	resultPath := state.ActiveResult
	v2Log := state.ActiveV2Log
	if v2Log == "" {
		v2Log = fmt.Sprintf("%s/issue-%d-recovered-v2.log", e.resultsDir, e.issue)
	}
	rc := e.dispatchResult(state, resultPath, v2Log)
	if state.LastResult == resultPath {
		e.clearActive(state)
		if err := saveStateAtomic(e.statePath, *state); err != nil {
			return false, 15
		}
		_ = appendHistory(e.historyPath, map[string]any{
			"turn_key": resultPath, "phase": "recovery", "recovered_at": utcNow(), "status": state.Status, "rc": rc,
		})
		return true, rc
	}
	return false, rc
}

func (e *engine) reconcileRecoveredThread(state *RuntimeState) error {
	found := threadFromEvents(state.ActiveEvents)
	if found == "" {
		return fmt.Errorf("completed %s turn has no valid thread.started identity", state.ActiveMode)
	}
	previous := state.ActivePreviousThread
	switch state.ActiveMode {
	case "start":
		if state.ThreadID == "" {
			state.ThreadID = found
			state.Status = "RUNNING"
			return saveStateAtomic(e.statePath, *state)
		}
		if state.ThreadID != found {
			return fmt.Errorf("start event thread %s does not match persisted %s", found, state.ThreadID)
		}
	case "resume":
		if previous == "" || found != previous || state.ThreadID != previous {
			return fmt.Errorf("resume event thread %s does not match expected %s", found, previous)
		}
	case "rotate":
		if previous == "" || found == previous {
			return errors.New("rotate did not establish a fresh thread")
		}
		if state.ThreadID == previous {
			state.ThreadHistory = append(state.ThreadHistory, ThreadHistoryEntry{
				ThreadID: state.ThreadID, Model: state.Model, Reasoning: state.Reasoning,
				TurnCount: state.ThreadTurnCount, RotatedAt: utcNow(), Reason: state.ActiveRotationReason,
			})
			state.ThreadID = found
			state.Model = firstNonEmpty(state.ActiveModel, state.Model)
			state.Reasoning = firstNonEmpty(state.ActiveReasoning, state.Reasoning)
			state.ThreadTurnCount = 0
			state.ThreadGeneration++
			state.Status = "ROTATED"
			return saveStateAtomic(e.statePath, *state)
		}
		if state.ThreadID != found {
			return fmt.Errorf("rotate event thread %s does not match persisted %s", found, state.ThreadID)
		}
	default:
		return fmt.Errorf("unknown active turn mode %q", state.ActiveMode)
	}
	return nil
}

func (e *engine) stopCommand() int {
	state, err := loadState(e.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No runtime state for Issue #%d.\n", e.issue)
			return 0
		}
		e.ui.errorf("load runtime state: %v", err)
		return 1
	}
	if state.ActiveMode == "" || state.ActivePID == nil {
		fmt.Printf("Issue #%d has no active Host turn.\n", e.issue)
		return e.recoverAfterStop()
	}
	pid := *state.ActivePID
	if !processExists(pid) {
		fmt.Printf("Recorded Issue #%d turn is already dead; reconciling durable state.\n", e.issue)
		return e.recoverAfterStop()
	}
	identity, owned := stateOwnsProcess(state)
	if !owned {
		e.ui.errorf("refusing stop: persisted pid=%d is alive but ownership cannot be proven for Issue #%d", pid, e.issue)
		return 1
	}
	fmt.Printf("Stopping Issue #%d %s turn (pid=%d pgid=%d)…\n", e.issue, state.ActiveMode, pid, identity.PGID)
	if err := syscall.Kill(-identity.PGID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		e.ui.errorf("signal owned process group: %v", err)
		return 1
	}
	deadline := time.Now().Add(5 * time.Second)
	for processExists(pid) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if processExists(pid) {
		e.ui.warnf(os.Stderr, "owned process group did not settle after TERM; escalating to KILL")
		_ = syscall.Kill(-identity.PGID, syscall.SIGKILL)
		for i := 0; i < 20 && processExists(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return e.recoverAfterStop()
}

func (e *engine) recoverAfterStop() int {
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		state, err := loadState(e.statePath)
		if err == nil && state.ActiveMode == "" && state.ActivePID == nil {
			e.ui.successf(os.Stdout, "Issue #%d turn stopped; durable state already reconciled", e.issue)
			return 0
		}
		lock, lockErr := acquireIssueLock(e.repo, e.issue)
		if lockErr == nil {
			state, loadErr := loadState(e.statePath)
			if loadErr == nil {
				recovered, rc := e.recoverActive(&state)
				_ = lock.Close()
				if recovered {
					e.ui.successf(os.Stdout, "Issue #%d stopped and recovered to %s", e.issue, state.Status)
					return 0
				}
				if rc != 0 && rc != 3 {
					return rc
				}
			} else {
				_ = lock.Close()
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	e.ui.errorf("stop completed signal delivery but durable state could not be reconciled")
	return 15
}

func removeIfPresent(path string) {
	if strings.TrimSpace(path) != "" {
		_ = os.Remove(path)
	}
}
