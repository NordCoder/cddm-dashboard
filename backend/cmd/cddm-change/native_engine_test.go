package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerEnvStripsHostCredentials(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret-gh")
	t.Setenv("GITHUB_TOKEN", "secret-github")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "secret-enterprise")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh.sock")
	t.Setenv("GIT_ASKPASS", "/tmp/askpass")

	e := &engine{workerHome: filepath.Join(t.TempDir(), "worker")}
	env := strings.Join(e.workerEnv(), "\n")
	for _, forbidden := range []string{"GH_TOKEN=", "GITHUB_TOKEN=", "GITHUB_ENTERPRISE_TOKEN=", "SSH_AUTH_SOCK=", "GIT_ASKPASS="} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("worker env contains %s", forbidden)
		}
	}
	for _, required := range []string{"GIT_CONFIG_GLOBAL=/dev/null", "CODEX_HOME=", "HOME="} {
		if !strings.Contains(env, required) {
			t.Fatalf("worker env missing %s", required)
		}
	}
}

func TestRecoverDurable143DoesNotDispatch(t *testing.T) {
	root := t.TempDir()
	statePath, historyPath, results := statePaths(root, 42)
	if err := os.MkdirAll(results, 0o755); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(results, "events.jsonl")
	result := filepath.Join(results, "result.json")
	exit := filepath.Join(results, "exit-status")
	if err := os.WriteFile(events, []byte("{\"type\":\"thread.started\",\"thread_id\":\"thread-a\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result, []byte("this must not be dispatched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exit, []byte("143\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := RuntimeState{
		Version: 4, Issue: 42, ThreadID: "thread-a", Status: "RUNNING", ThreadGeneration: 1,
		ActiveMode: "resume", ActiveEvents: events, ActiveResult: result, ActiveExitStatus: exit,
		ActivePreviousThread: "thread-a", ActiveModel: "luna", ActiveReasoning: "medium",
	}
	if err := saveStateAtomic(statePath, state); err != nil {
		t.Fatal(err)
	}
	e := &engine{ui: newUI(ColorNever), repo: root, issue: 42, statePath: statePath, historyPath: historyPath, resultsDir: results}
	loaded, _ := loadState(statePath)
	recovered, rc := e.recoverActive(&loaded)
	if !recovered || rc != 143 {
		t.Fatalf("recovered=%v rc=%d", recovered, rc)
	}
	if loaded.Status != "TURN_FAILED" || loaded.ActiveMode != "" || loaded.ActivePID != nil {
		t.Fatalf("state after recovery = %#v", loaded)
	}
	if loaded.LastResultRC == nil || *loaded.LastResultRC != 143 || loaded.LastResult != result {
		t.Fatalf("result disposition = %#v", loaded)
	}
}

func TestThreadBindingStartResumeRotate(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	e := &engine{ui: newUI(ColorNever), statePath: statePath}
	state := RuntimeState{Version: 4, ThreadGeneration: 1, Model: "terra", Reasoning: "medium"}
	if err := saveStateAtomic(statePath, state); err != nil {
		t.Fatal(err)
	}
	if err := e.bindLiveThread(&state, "start", "", "thread-a", "terra", "medium", ""); err != nil {
		t.Fatal(err)
	}
	if state.ThreadID != "thread-a" {
		t.Fatalf("thread = %s", state.ThreadID)
	}
	if err := e.bindLiveThread(&state, "resume", "thread-a", "thread-a", "luna", "medium", ""); err != nil {
		t.Fatal(err)
	}
	state.ThreadTurnCount = 2
	if err := e.bindLiveThread(&state, "rotate", "thread-a", "thread-b", "luna", "high", "context refresh"); err != nil {
		t.Fatal(err)
	}
	if state.ThreadID != "thread-b" || state.ThreadGeneration != 2 || len(state.ThreadHistory) != 1 || state.ThreadHistory[0].ThreadID != "thread-a" {
		t.Fatalf("rotated state = %#v", state)
	}
	if err := e.bindLiveThread(&state, "resume", "thread-b", "wrong", "luna", "high", ""); err == nil {
		t.Fatal("wrong resume thread accepted")
	}
}

func TestValidateWorkerResultRejectsExtraKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	data := `{"status":"CONTINUE","summary":"ok","verify":"ok","blocker":"none","extra":true}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := validateWorkerResult(path); err == nil {
		t.Fatal("extra result key accepted")
	}
}

func TestNoOpRejectsDirtyWorktree(t *testing.T) {
	repo := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(t.TempDir(), "result.json")
	writeResult(t, result, "NO_OP")
	e := &engine{ui: newUI(ColorNever), worktree: repo, statePath: filepath.Join(t.TempDir(), "state.json")}
	state := RuntimeState{Version: 4, Status: "RUNNING"}
	if rc := e.dispatchResult(&state, result, filepath.Join(t.TempDir(), "v2.log")); rc != 14 {
		t.Fatalf("rc=%d, want 14", rc)
	}
	if state.Status != "INVALID_NO_OP" {
		t.Fatalf("status=%s", state.Status)
	}
}

func TestCandidateV2FailurePublishesNothing(t *testing.T) {
	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := t.TempDir()
	writeTool(t, fake, "gofmt", "exit 0")
	writeTool(t, fake, "go", "exit 7")
	writeTool(t, fake, "npm", "exit 0")
	writeTool(t, fake, "docker", "exit 0")
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := filepath.Join(t.TempDir(), "result.json")
	writeResult(t, result, "CANDIDATE_READY")
	statePath := filepath.Join(t.TempDir(), "state.json")
	e := &engine{ui: newUI(ColorNever), issue: 42, worktree: repo, statePath: statePath}
	state := RuntimeState{Version: 4, Status: "RUNNING"}
	rc := e.commitAndPublishCandidate(&state, result, filepath.Join(t.TempDir(), "v2.log"))
	if rc != 4 || state.Status != "V2_FAILED" || state.CandidateHead != "" {
		t.Fatalf("rc=%d state=%#v", rc, state)
	}
}

func TestCandidatePassCreatesExactCommitPushAndPR(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	runGit(t, root, "init", "--bare", bare)
	repo := filepath.Join(root, "worktree")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "change/42")
	runGit(t, repo, "config", "user.name", "Runtime Test")
	runGit(t, repo, "config", "user.email", "runtime@example.test")
	for _, dir := range []string{"backend", "web"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "base")
	runGit(t, repo, "remote", "add", "origin", bare)
	runGit(t, repo, "push", "-u", "origin", "HEAD:change/42")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := t.TempDir()
	for _, tool := range []string{"gofmt", "go", "npm", "docker"} {
		writeTool(t, fake, tool, "exit 0")
	}
	writeTool(t, fake, "gh", `
case "$1 $2" in
  "pr list") echo '[{"number":123}]' ;;
  "pr view") head="$(git -C "$TEST_WORKTREE" rev-parse HEAD)"; printf '{"headRefOid":"%s"}\n' "$head" ;;
  *) exit 91 ;;
esac`)
	t.Setenv("TEST_WORKTREE", repo)
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := filepath.Join(root, "result.json")
	writeResult(t, result, "CANDIDATE_READY")
	statePath := filepath.Join(root, "state.json")
	e := &engine{
		ui: newUI(ColorNever), repo: root, issue: 42, branch: "change/42", worktree: repo,
		statePath: statePath, resultsDir: root, originURL: bare, contract: "none",
	}
	state := RuntimeState{Version: 4, Issue: 42, Branch: "change/42", Worktree: repo, Status: "RUNNING"}
	if rc := e.commitAndPublishCandidate(&state, result, filepath.Join(root, "v2.log")); rc != 0 {
		t.Fatalf("candidate rc=%d state=%#v", rc, state)
	}
	if state.Status != "CANDIDATE" || state.PR == nil || *state.PR != 123 || state.CandidateHead == "" {
		t.Fatalf("candidate state=%#v", state)
	}
	remote := strings.TrimSpace(runGitOutput(t, repo, "ls-remote", bare, "refs/heads/change/42"))
	if !strings.HasPrefix(remote, state.CandidateHead+"\t") {
		t.Fatalf("remote=%q head=%s", remote, state.CandidateHead)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Runtime Test")
	runGit(t, repo, "config", "user.email", "runtime@example.test")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "base")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func writeTool(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeResult(t *testing.T, path, status string) {
	t.Helper()
	data, _ := json.Marshal(workerResult{Status: status, Summary: "ok", Verify: "ok", Blocker: "none"})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
