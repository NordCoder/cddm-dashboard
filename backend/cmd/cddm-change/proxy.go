package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func commandProxy(ui *UI, args []string) int {
	repo, issue, mode, stall, command, err := parseProxyArgs(args)
	if err != nil {
		ui.errorf("%v", err)
		return 2
	}
	if len(command) == 0 {
		ui.errorf("proxy command is required after --")
		return 2
	}

	child := exec.Command(command[0], command[1:]...)
	child.Stdin = os.Stdin
	child.Stderr = os.Stderr
	stdout, err := child.StdoutPipe()
	if err != nil {
		ui.errorf("prepare Codex stdout: %v", err)
		return 1
	}
	// Consequential child starts before telemetry or presentation I/O.
	if err := child.Start(); err != nil {
		ui.errorf("start Codex child: %v", err)
		return 1
	}

	statePath, historyPath, _ := statePaths(repo, issue)
	state, _ := loadState(statePath)
	turnKey := firstNonEmpty(state.ActiveResult, state.LastResult, state.ActiveEvents, "unknown:"+utcNow())
	model := firstNonEmpty(state.ActiveModel, state.Model, "model?")
	reasoning := firstNonEmpty(state.ActiveReasoning, state.Reasoning, "reasoning?")
	actualMode := firstNonEmpty(state.ActiveMode, mode, "unknown")
	start := time.Now()
	_ = appendHistory(historyPath, map[string]any{
		"turn_key": turnKey, "phase": "start", "started_at": utcNow(), "mode": actualMode,
		"model": model, "reasoning": reasoning, "events": state.ActiveEvents, "result": state.ActiveResult,
		"v2_log": state.ActiveV2Log, "exit_status": state.ActiveExitStatus,
	})
	ui.header(os.Stderr, issue, fmt.Sprintf("%s · %s / %s", actualMode, model, reasoning))

	type lineResult struct {
		line string
		err  error
	}
	lines := make(chan lineResult, 32)
	go func() {
		defer close(lines)
		r := bufio.NewReaderSize(stdout, 128*1024)
		for {
			line, err := r.ReadString('\n')
			if line != "" {
				lines <- lineResult{line: line}
			}
			if err != nil {
				if err != io.EOF {
					lines <- lineResult{err: err}
				}
				return
			}
		}
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- child.Wait() }()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastEvent := time.Now()
	lastWarn := time.Time{}
	var usage Usage
	var childErr error
	childDone := false
	linesDone := false

	for !(childDone && linesDone) {
		select {
		case lr, ok := <-lines:
			if !ok {
				linesDone = true
				continue
			}
			if lr.err != nil {
				ui.warnf(os.Stderr, "event stream read failed: %v · waiting for Codex completion", lr.err)
				continue
			}
			_, _ = io.WriteString(os.Stdout, lr.line)
			lastEvent = time.Now()
			lastWarn = time.Time{}
			trimmed := strings.TrimSuffix(lr.line, "\n")
			var event map[string]any
			if json.Unmarshal([]byte(trimmed), &event) == nil {
				parseUsage(event, &usage)
			}
			ui.printEvent(os.Stderr, time.Since(start), parseRenderedEvent(trimmed))
		case err := <-waitCh:
			childErr = err
			childDone = true
		case sig := <-sigCh:
			if child.Process != nil {
				_ = child.Process.Signal(sig)
			}
		case <-ticker.C:
			if stall > 0 && time.Since(lastEvent) >= time.Duration(stall)*time.Second {
				if lastWarn.IsZero() || time.Since(lastWarn) >= time.Duration(stall)*time.Second {
					ui.warnf(os.Stderr, "no Codex events for %s · process still alive · observation only", humanDuration(time.Since(lastEvent)))
					lastWarn = time.Now()
				}
			}
		}
	}

	rc := exitCode(childErr)
	resultStatus := ""
	if state.ActiveResult != "" {
		if obj := loadObject(state.ActiveResult); obj != nil {
			resultStatus = stringValue(obj["status"])
		}
	}
	_ = appendHistory(historyPath, map[string]any{
		"turn_key": turnKey, "phase": "finish", "ended_at": utcNow(), "rc": rc,
		"result_status": resultStatus, "usage": usage,
	})
	ok := rc == 0
	kind := "DONE"
	if !ok {
		kind = "ERROR"
	}
	ui.printEvent(os.Stderr, time.Since(start), renderedEvent{Kind: kind, Text: fmt.Sprintf("Codex exit=%d%s", rc, optionalSuffix(resultStatus)), Success: &ok})
	return rc
}

func parseProxyArgs(args []string) (repo string, issue int, mode string, stall int, command []string, err error) {
	stall = envStallSeconds()
	mode = "unknown"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 >= len(args) { return "", 0, "", 0, nil, fmt.Errorf("--repo requires value") }
			repo = args[i+1]; i++
		case "--issue":
			if i+1 >= len(args) { return "", 0, "", 0, nil, fmt.Errorf("--issue requires value") }
			issue, err = parseIssue(args[i+1]); i++
			if err != nil { return "", 0, "", 0, nil, err }
		case "--mode":
			if i+1 >= len(args) { return "", 0, "", 0, nil, fmt.Errorf("--mode requires value") }
			mode = args[i+1]; i++
		case "--stall-seconds":
			if i+1 >= len(args) { return "", 0, "", 0, nil, fmt.Errorf("--stall-seconds requires value") }
			stall, err = strconv.Atoi(args[i+1]); i++
			if err != nil || stall < 0 { return "", 0, "", 0, nil, fmt.Errorf("invalid --stall-seconds") }
		case "--":
			command = args[i+1:]
			return
		default:
			return "", 0, "", 0, nil, fmt.Errorf("unknown proxy option %q", args[i])
		}
	}
	if repo == "" || issue == 0 {
		err = fmt.Errorf("proxy requires --repo and --issue")
	}
	return
}

func commandV2Tee(ui *UI, args []string) int {
	if len(args) != 2 || args[0] != "--log" {
		fmt.Fprintln(os.Stderr, "usage: __v2tee --log <path>")
		return 2
	}
	logPath := args[1]
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		ui.errorf("create V2 log directory: %v", err)
		return 1
	}
	log, err := os.Create(logPath)
	if err != nil {
		ui.errorf("create V2 log: %v", err)
		return 1
	}
	defer log.Close()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var phase string
	var tail []string
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(log, line)
		if strings.HasPrefix(line, "@@CDDM_V2@@|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 4 && parts[1] == "START" {
				phase = parts[2]
				tail = tail[:0]
				fmt.Fprintf(os.Stdout, "%s %s\n", ui.style(os.Stdout, ansiYellow, "› RUN"), phase)
				continue
			}
			if len(parts) >= 5 && parts[1] == "END" {
				rc, _ := strconv.Atoi(parts[3])
				duration := parts[4]
				if rc == 0 {
					fmt.Fprintf(os.Stdout, "%s %-28s %ss\n", ui.style(os.Stdout, ansiGreen, "✓ PASS"), parts[2], duration)
				} else {
					fmt.Fprintf(os.Stdout, "%s %-28s rc=%d · %ss\n", ui.style(os.Stdout, ansiRed, "✗ FAIL"), parts[2], rc, duration)
					for _, raw := range tail {
						fmt.Fprintf(os.Stdout, "  %s\n", ui.style(os.Stdout, ansiDim, raw))
					}
				}
				phase = ""
				continue
			}
		}
		if phase != "" {
			tail = append(tail, line)
			if len(tail) > 12 {
				tail = tail[len(tail)-12:]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		ui.errorf("read V2 stream: %v", err)
		return 1
	}
	return 0
}

func commandRecordRecovery(args []string) int {
	var repo string
	var issue int
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 < len(args) { repo = args[i+1]; i++ }
		case "--issue":
			if i+1 < len(args) { issue, _ = strconv.Atoi(args[i+1]); i++ }
		}
	}
	if repo == "" || issue <= 0 {
		return 2
	}
	statePath, historyPath, _ := statePaths(repo, issue)
	state, err := loadState(statePath)
	if err != nil {
		return 0
	}
	key := firstNonEmpty(state.LastResult, state.ActiveResult, state.ActiveEvents)
	if key == "" {
		return 0
	}
	row := map[string]any{"turn_key": key, "phase": "recovery", "recovered_at": utcNow(), "status": state.Status}
	if state.LastResultRC != nil {
		row["rc"] = *state.LastResultRC
	}
	_ = appendHistory(historyPath, row)
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" { return v }
	}
	return ""
}

func optionalSuffix(s string) string {
	if s == "" { return "" }
	return " · " + s
}
