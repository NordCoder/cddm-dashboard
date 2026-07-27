package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func commandStatus(ui *UI, repo string, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: status <issue> [--json]")
		return 2
	}
	issue, err := parseIssue(args[0])
	if err != nil {
		ui.errorf("%v", err)
		return 2
	}
	jsonMode := len(args) > 1 && args[1] == "--json"
	statePath, historyPath, _ := statePaths(repo, issue)
	if jsonMode {
		data, err := os.ReadFile(statePath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("{}\n")
				return 0
			}
			ui.errorf("read runtime state: %v", err)
			return 1
		}
		_, _ = os.Stdout.Write(data)
		if len(data) == 0 || data[len(data)-1] != '\n' {
			fmt.Println()
		}
		return 0
	}
	state, err := loadState(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No runtime state for Issue #%d.\n", issue)
			return 0
		}
		ui.errorf("read runtime state: %v", err)
		return 1
	}
	printStatusDashboard(ui, os.Stdout, issue, state, historyPath)
	return 0
}

func printStatusDashboard(ui *UI, w io.Writer, issue int, state RuntimeState, historyPath string) {
	ui.header(w, issue, ui.badge(w, state.Status))
	fmt.Fprintf(w, "%s %s\n", ui.label(w, "Thread"), defaultString(state.ThreadID, "—"))
	fmt.Fprintf(w, "%s generation %d · %d current / %d total turns\n", ui.label(w, "Context"), max(state.ThreadGeneration, 1), state.ThreadTurnCount, state.TotalTurnCount)
	fmt.Fprintf(w, "%s %s / %s\n", ui.label(w, "Model"), defaultString(state.Model, "—"), defaultString(state.Reasoning, "—"))

	if state.ActiveMode != "" {
		started := activeStarted(historyPath, state.ActiveResult)
		elapsed := time.Duration(-1)
		if !started.IsZero() {
			elapsed = time.Since(started)
		}
		age := fileAge(state.ActiveEvents)
		proc := "not observed"
		if processAlive(state.ActivePID) {
			proc = ui.style(w, ansiGreen, "alive")
		}
		ui.section(w, "Active turn")
		fmt.Fprintf(w, "%s %s\n", ui.label(w, "Mode"), state.ActiveMode)
		fmt.Fprintf(w, "%s %s\n", ui.label(w, "Elapsed"), humanDuration(elapsed))
		if age >= 0 {
			fmt.Fprintf(w, "%s %s ago\n", ui.label(w, "Last event"), humanDuration(age))
		} else {
			fmt.Fprintf(w, "%s unknown\n", ui.label(w, "Last event"))
		}
		pid := ""
		if state.ActivePID != nil {
			pid = fmt.Sprintf(" · pid=%d", *state.ActivePID)
		}
		fmt.Fprintf(w, "%s %s%s\n", ui.label(w, "Process"), proc, pid)
	}

	ui.section(w, "Candidate")
	fmt.Fprintf(w, "%s %s\n", ui.label(w, "Head"), defaultString(state.CandidateHead, "—"))
	if state.PR != nil {
		fmt.Fprintf(w, "%s #%d\n", ui.label(w, "PR"), *state.PR)
	} else {
		fmt.Fprintf(w, "%s —\n", ui.label(w, "PR"))
	}

	changes := gitStatus(state.Worktree)
	ui.section(w, "Worktree")
	if len(changes) == 0 {
		fmt.Fprintf(w, "%s %s\n", ui.label(w, "Changes"), ui.style(w, ansiGreen, "clean"))
		return
	}
	fmt.Fprintf(w, "%s %d path(s)\n", ui.label(w, "Changes"), len(changes))
	for i, line := range changes {
		if i >= 10 {
			fmt.Fprintf(w, "  %s\n", ui.style(w, ansiDim, fmt.Sprintf("… %d more", len(changes)-i)))
			break
		}
		fmt.Fprintf(w, "  %s\n", line)
	}
}

func commandLogs(ui *UI, repo string, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: logs <issue> [--raw|--v2]")
		return 2
	}
	issue, err := parseIssue(args[0])
	if err != nil {
		ui.errorf("%v", err)
		return 2
	}
	raw, v2 := false, false
	for _, arg := range args[1:] {
		switch arg {
		case "--raw":
			raw = true
		case "--v2":
			v2 = true
		default:
			ui.errorf("unknown logs option %q", arg)
			return 2
		}
	}
	statePath, _, _ := statePaths(repo, issue)
	state, _ := loadState(statePath)
	events, v2Path := latestPaths(repo, issue, state)
	path := events
	if v2 {
		path = v2Path
	}
	if !regularFile(path) {
		fmt.Printf("No %s log found for Issue #%d.\n", map[bool]string{true: "V2", false: "Codex event"}[v2], issue)
		return 0
	}
	if raw || v2 {
		data, err := os.ReadFile(path)
		if err != nil {
			ui.errorf("read log: %v", err)
			return 1
		}
		_, _ = os.Stdout.Write(data)
		return 0
	}
	ui.header(os.Stdout, issue, "Codex event log · "+filepath.Base(path))
	f, err := os.Open(path)
	if err != nil {
		ui.errorf("open event log: %v", err)
		return 1
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	start := time.Now()
	for s.Scan() {
		ui.printEvent(os.Stdout, time.Since(start), parseRenderedEvent(s.Text()))
	}
	return 0
}

func commandTurns(ui *UI, repo string, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: turns <issue> [--limit N]")
		return 2
	}
	issue, err := parseIssue(args[0])
	if err != nil {
		ui.errorf("%v", err)
		return 2
	}
	limit := 20
	for i := 1; i < len(args); i++ {
		if args[i] == "--limit" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				ui.errorf("--limit must be positive")
				return 2
			}
			limit = n
			i++
		} else {
			ui.errorf("unknown turns option %q", args[i])
			return 2
		}
	}
	_, historyPath, _ := statePaths(repo, issue)
	turns := readTurns(historyPath)
	if len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	ui.header(os.Stdout, issue, fmt.Sprintf("turn history · last %d", len(turns)))
	fmt.Fprintf(os.Stdout, "%s\n", ui.style(os.Stdout, ansiDim, "#   MODE     MODEL                   DURATION   RC    RESULT              TOKENS in/cache/out"))
	for i, turn := range turns {
		duration := "running"
		if !turn.StartedAt.IsZero() && !turn.EndedAt.IsZero() {
			duration = humanDuration(turn.EndedAt.Sub(turn.StartedAt))
		}
		rc := "—"
		if turn.RC != nil {
			rc = strconv.Itoa(*turn.RC)
		}
		result := defaultString(turn.ResultStatus, "—")
		tokens := fmt.Sprintf("%s/%s/%s", tokenValue(turn.Usage.Input), tokenValue(turn.Usage.Cached), tokenValue(turn.Usage.Output))
		model := defaultString(turn.Model, "—") + "/" + defaultString(turn.Reasoning, "—")
		fmt.Fprintf(os.Stdout, "%-3d %-8s %-23s %-10s %-5s %-19s %s\n", i+1, defaultString(turn.Mode, "—"), trimWidth(model, 23), duration, rc, trimWidth(result, 19), tokens)
	}
	return 0
}

func commandWatch(ui *UI, repo string, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: watch <issue> [--stall-seconds N]")
		return 2
	}
	issue, err := parseIssue(args[0])
	if err != nil {
		ui.errorf("%v", err)
		return 2
	}
	stall := envStallSeconds()
	for i := 1; i < len(args); i++ {
		if args[i] == "--stall-seconds" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 0 {
				ui.errorf("--stall-seconds must be non-negative")
				return 2
			}
			stall = n
			i++
		} else {
			ui.errorf("unknown watch option %q", args[i])
			return 2
		}
	}
	statePath, _, _ := statePaths(repo, issue)
	state, err := loadState(statePath)
	if err != nil {
		fmt.Printf("No runtime state for Issue #%d.\n", issue)
		return 0
	}
	ui.header(os.Stdout, issue, "live observer · Ctrl+C exits observer only")
	fmt.Fprintf(os.Stdout, "%s %s\n\n", ui.label(os.Stdout, "State"), ui.badge(os.Stdout, state.Status))

	current := ""
	var offset int64
	start := time.Now()
	lastEvent := time.Now()
	lastWarn := time.Time{}
	lastStatus := state.Status
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		state, err = loadState(statePath)
		if err != nil {
			continue
		}
		if state.Status != lastStatus {
			fmt.Fprintf(os.Stdout, "%s %s → %s\n", ui.style(os.Stdout, ansiBlue, "● STATE"), lastStatus, ui.badge(os.Stdout, state.Status))
			lastStatus = state.Status
		}
		if state.ActiveEvents != "" && state.ActiveEvents != current {
			current = state.ActiveEvents
			offset = 0
			lastEvent = time.Now()
		}
		if current != "" {
			newOffset, count := followEvents(ui, current, offset, start)
			if count > 0 {
				offset = newOffset
				lastEvent = time.Now()
				lastWarn = time.Time{}
			}
		}
		if stall > 0 && state.ActiveMode != "" && time.Since(lastEvent) >= time.Duration(stall)*time.Second {
			if lastWarn.IsZero() || time.Since(lastWarn) >= time.Duration(stall)*time.Second {
				ui.warnf(os.Stdout, "no Codex events for %s · process %s · observation only", humanDuration(time.Since(lastEvent)), map[bool]string{true: "alive", false: "not observed"}[processAlive(state.ActivePID)])
				lastWarn = time.Now()
			}
		}
		if state.ActiveMode == "" && current != "" {
			followEvents(ui, current, offset, start) // final best-effort drain
			fmt.Fprintf(os.Stdout, "\n%s\n", ui.style(os.Stdout, ansiDim, "observer: active turn ended"))
			return 0
		}
	}
	return 0
}

func followEvents(ui *UI, path string, offset int64, started time.Time) (int64, int) {
	f, err := os.Open(path)
	if err != nil {
		return offset, 0
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, 0
	}
	r := bufio.NewReader(f)
	count := 0
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			ui.printEvent(os.Stdout, time.Since(started), parseRenderedEvent(strings.TrimSuffix(line, "\n")))
			count++
			offset += int64(len(line))
		}
		if err != nil {
			break
		}
	}
	return offset, count
}

func activeStarted(historyPath, activeResult string) time.Time {
	turns := readTurns(historyPath)
	for i := len(turns) - 1; i >= 0; i-- {
		if activeResult != "" && turns[i].Result == activeResult {
			return turns[i].StartedAt
		}
	}
	return time.Time{}
}

func fileAge(path string) time.Duration {
	if path == "" {
		return -1
	}
	st, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return time.Since(st.ModTime())
}

func tokenValue(v *int) string {
	if v == nil {
		return "?"
	}
	return strconv.Itoa(*v)
}

func trimWidth(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return s[:width]
	}
	return s[:width-1] + "…"
}

func envStallSeconds() int {
	v := strings.TrimSpace(os.Getenv("CDDM_STALL_SECONDS"))
	if v == "" {
		return 60
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 60
	}
	return n
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Ensure status --json remains a machine-readable compatibility surface.
var _ = json.Valid
