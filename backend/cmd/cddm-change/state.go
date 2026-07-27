package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type RuntimeState struct {
	Version          int    `json:"version"`
	Issue            int    `json:"issue"`
	Branch           string `json:"branch"`
	Worktree         string `json:"worktree"`
	ThreadID         string `json:"thread_id"`
	Model            string `json:"model"`
	Reasoning        string `json:"reasoning"`
	Contract         string `json:"contract"`
	Status           string `json:"status"`
	ThreadTurnCount  int    `json:"thread_turn_count"`
	TotalTurnCount   int    `json:"total_turn_count"`
	ThreadGeneration int    `json:"thread_generation"`
	CandidateHead    string `json:"candidate_head"`
	PR               *int   `json:"pr"`
	ActivePID        *int   `json:"active_pid"`
	ActiveMode       string `json:"active_mode"`
	ActiveEvents     string `json:"active_events"`
	ActiveResult     string `json:"active_result"`
	ActiveV2Log      string `json:"active_v2_log"`
	ActiveExitStatus string `json:"active_exit_status"`
	ActiveModel      string `json:"active_model"`
	ActiveReasoning  string `json:"active_reasoning"`
	LastResult       string `json:"last_result"`
	LastResultRC     *int   `json:"last_result_rc"`
	UpdatedAt        string `json:"updated_at"`
}

type Usage struct {
	Input  *int `json:"input_tokens"`
	Cached *int `json:"cached_input_tokens"`
	Output *int `json:"output_tokens"`
}

type Turn struct {
	Key          string
	StartedAt    time.Time
	EndedAt      time.Time
	Mode         string
	Model        string
	Reasoning    string
	Events       string
	Result       string
	V2Log        string
	ExitStatus   string
	RC           *int
	ResultStatus string
	Usage        Usage
}

func statePaths(repo string, issue int) (string, string, string) {
	runtimeDir := filepath.Join(repo, ".worktrees", "runtime")
	resultsDir := filepath.Join(repo, ".worktrees", "results")
	return filepath.Join(runtimeDir, fmt.Sprintf("issue-%d.json", issue)), filepath.Join(runtimeDir, fmt.Sprintf("issue-%d-turns.jsonl", issue)), resultsDir
}

func loadState(path string) (RuntimeState, error) {
	var state RuntimeState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func loadObject(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func appendHistory(path string, row map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
	data, err := json.Marshal(row)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func readTurns(path string) []Turn {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	order := make([]string, 0)
	folded := map[string]*Turn{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		var row map[string]any
		if json.Unmarshal(s.Bytes(), &row) != nil {
			continue
		}
		key, _ := row["turn_key"].(string)
		if key == "" {
			continue
		}
		t := folded[key]
		if t == nil {
			t = &Turn{Key: key}
			folded[key] = t
			order = append(order, key)
		}
		mergeTurn(t, row)
	}
	out := make([]Turn, 0, len(order))
	for _, key := range order {
		out = append(out, *folded[key])
	}
	return out
}

func mergeTurn(t *Turn, row map[string]any) {
	if v, ok := row["started_at"].(string); ok {
		t.StartedAt = parseTime(v)
	}
	if v, ok := row["ended_at"].(string); ok {
		t.EndedAt = parseTime(v)
	}
	for key, target := range map[string]*string{
		"mode": &t.Mode, "model": &t.Model, "reasoning": &t.Reasoning, "events": &t.Events,
		"result": &t.Result, "v2_log": &t.V2Log, "exit_status": &t.ExitStatus, "result_status": &t.ResultStatus,
	} {
		if v, ok := row[key].(string); ok && v != "" {
			*target = v
		}
	}
	if v, ok := numberInt(row["rc"]); ok {
		t.RC = ptr(v)
	}
	if u, ok := row["usage"].(map[string]any); ok {
		if v, ok := numberInt(u["input_tokens"]); ok {
			t.Usage.Input = ptr(v)
		}
		if v, ok := numberInt(u["cached_input_tokens"]); ok {
			t.Usage.Cached = ptr(v)
		}
		if v, ok := numberInt(u["output_tokens"]); ok {
			t.Usage.Output = ptr(v)
		}
	}
}

func numberInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

func ptr(v int) *int { return &v }

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339, v)
	return t
}

func utcNow() string { return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339) }

func humanDuration(d time.Duration) string {
	if d < 0 {
		return "unknown"
	}
	total := int(d.Round(time.Second).Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func processAlive(pid *int) bool {
	if pid == nil || *pid <= 0 {
		return false
	}
	return syscall.Kill(*pid, 0) == nil
}

func latestPaths(repo string, issue int, state RuntimeState) (string, string) {
	_, historyPath, resultsDir := statePaths(repo, issue)
	events := state.ActiveEvents
	v2 := state.ActiveV2Log
	if !regularFile(events) || !regularFile(v2) {
		turns := readTurns(historyPath)
		for i := len(turns) - 1; i >= 0; i-- {
			if !regularFile(events) && regularFile(turns[i].Events) {
				events = turns[i].Events
			}
			if !regularFile(v2) && regularFile(turns[i].V2Log) {
				v2 = turns[i].V2Log
			}
		}
	}
	if !regularFile(events) {
		events = latestMatching(resultsDir, fmt.Sprintf("issue-%d-", issue), ".jsonl")
	}
	if !regularFile(v2) {
		v2 = latestMatching(resultsDir, fmt.Sprintf("issue-%d-", issue), ".log")
	}
	return events, v2
}

func regularFile(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func latestMatching(dir, prefix, suffix string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type candidate struct {
		path string
		mod  time.Time
	}
	var list []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			list = append(list, candidate{filepath.Join(dir, entry.Name()), info.ModTime()})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.After(list[j].mod) })
	if len(list) == 0 {
		return ""
	}
	return list[0].path
}

func gitStatus(worktree string) []string {
	if worktree == "" {
		return nil
	}
	cmd := exec.Command("git", "-C", worktree, "status", "--short")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func parseIssue(s string) (int, error) {
	i, err := strconv.Atoi(s)
	if err != nil || i <= 0 {
		return 0, errors.New("issue must be a positive integer")
	}
	return i, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
	}
	return 1
}
