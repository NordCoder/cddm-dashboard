package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTurnStartResumeRotateWithFakeCodex(t *testing.T) {
	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".codex", "config.toml"), []byte("[permissions]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaDir := filepath.Join(repo, ".codex", "schemas")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(schemaDir, "change-turn-result.json"), []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	hostCodex := filepath.Join(t.TempDir(), "host-codex")
	if err := os.MkdirAll(hostCodex, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostCodex, "auth.json"), []byte(`{"token":"test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", hostCodex)

	fakeBin := t.TempDir()
	writeTool(t, fakeBin, "codex", `
result=""
prev=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-last-message) result="$2"; shift 2 ;;
    resume) prev="$2"; shift 2 ;;
    *) shift ;;
  esac
done
thread="${TEST_FAKE_THREAD:?}"
if [[ -n "$prev" && "$prev" != "$thread" ]]; then
  echo "resume mismatch: got=$prev want=$thread" >&2
  exit 41
fi
printf '{"type":"thread.started","thread_id":"%s"}\n' "$thread"
printf '{"type":"item.started","item":{"type":"command_execution","command":"fake-check"}}\n'
printf '{"type":"item.completed","item":{"type":"command_execution","command":"fake-check","exit_code":0}}\n'
printf '{"status":"CONTINUE","summary":"ok","verify":"ok","blocker":"none"}\n' > "$result"
`)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	statePath, historyPath, resultsDir := statePaths(repo, 42)
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	e := &engine{
		ui: newUI(ColorNever), repo: repo, issue: 42, branch: "change/42", worktree: repo,
		workerHome: filepath.Join(repo, ".cddm-worker-home"), statePath: statePath,
		historyPath: historyPath, resultsDir: resultsDir, contract: "none",
	}
	state := RuntimeState{
		Version: 4, Issue: 42, Branch: "change/42", Worktree: repo, Model: "luna", Reasoning: "medium",
		Status: "INITIALIZING", ThreadGeneration: 1,
	}
	if err := saveStateAtomic(statePath, state); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_FAKE_THREAD", "thread-a")
	if rc := e.runTurn("start", "", "luna", "medium", "prompt", ""); rc != 0 {
		t.Fatalf("start rc=%d", rc)
	}
	afterStart, _ := loadState(statePath)
	if afterStart.ThreadID != "thread-a" || afterStart.Status != "CONTINUE" || afterStart.ThreadTurnCount != 1 || afterStart.ActiveMode != "" {
		t.Fatalf("after start = %#v", afterStart)
	}

	if rc := e.runTurn("resume", "thread-a", "luna", "medium", "prompt", ""); rc != 0 {
		t.Fatalf("resume rc=%d", rc)
	}
	afterResume, _ := loadState(statePath)
	if afterResume.ThreadID != "thread-a" || afterResume.ThreadTurnCount != 2 {
		t.Fatalf("after resume = %#v", afterResume)
	}

	t.Setenv("TEST_FAKE_THREAD", "thread-b")
	if rc := e.runTurn("rotate", "thread-a", "luna", "high", "prompt", "context refresh"); rc != 0 {
		t.Fatalf("rotate rc=%d", rc)
	}
	afterRotate, _ := loadState(statePath)
	if afterRotate.ThreadID != "thread-b" || afterRotate.ThreadGeneration != 2 || afterRotate.ThreadTurnCount != 1 || afterRotate.TotalTurnCount != 3 {
		t.Fatalf("after rotate = %#v", afterRotate)
	}
	if len(afterRotate.ThreadHistory) != 1 || afterRotate.ThreadHistory[0].ThreadID != "thread-a" || afterRotate.ThreadHistory[0].TurnCount != 2 {
		t.Fatalf("thread history = %#v", afterRotate.ThreadHistory)
	}
}

func TestRecoverSuccessfulContinueExactlyOnce(t *testing.T) {
	root := t.TempDir()
	statePath, historyPath, results := statePaths(root, 77)
	if err := os.MkdirAll(results, 0o755); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(results, "events.jsonl")
	result := filepath.Join(results, "result.json")
	exit := filepath.Join(results, "exit-status")
	if err := os.WriteFile(events, []byte("{\"type\":\"thread.started\",\"thread_id\":\"thread-r\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeResult(t, result, "CONTINUE")
	if err := os.WriteFile(exit, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := RuntimeState{
		Version: 4, Issue: 77, ThreadID: "thread-r", Status: "RUNNING", ThreadGeneration: 1,
		ActiveMode: "resume", ActiveEvents: events, ActiveResult: result, ActiveExitStatus: exit,
		ActivePreviousThread: "thread-r", ActiveModel: "luna", ActiveReasoning: "medium",
	}
	if err := saveStateAtomic(statePath, state); err != nil {
		t.Fatal(err)
	}
	e := &engine{ui: newUI(ColorNever), repo: root, issue: 77, statePath: statePath, historyPath: historyPath, resultsDir: results}

	loaded, _ := loadState(statePath)
	recovered, rc := e.recoverActive(&loaded)
	if !recovered || rc != 0 || loaded.Status != "CONTINUE" || loaded.LastResult != result || loaded.ActiveMode != "" {
		t.Fatalf("first recovery: recovered=%v rc=%d state=%#v", recovered, rc, loaded)
	}
	loadedAgain, _ := loadState(statePath)
	recoveredAgain, rcAgain := e.recoverActive(&loadedAgain)
	if recoveredAgain || rcAgain != 0 || loadedAgain.TotalTurnCount != 1 {
		t.Fatalf("second recovery: recovered=%v rc=%d state=%#v", recoveredAgain, rcAgain, loadedAgain)
	}
}

func TestStopRefusesReusedPIDIdentity(t *testing.T) {
	root := t.TempDir()
	statePath, historyPath, results := statePaths(root, 88)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	pid := os.Getpid()
	identity, err := readProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	state := RuntimeState{
		Version: 4, Issue: 88, Status: "RUNNING", ActiveMode: "resume", ActivePID: &pid,
		ActivePGID: &identity.PGID, ActivePIDStartTicks: identity.StartTicks + "-wrong",
	}
	if err := saveStateAtomic(statePath, state); err != nil {
		t.Fatal(err)
	}
	e := &engine{ui: newUI(ColorNever), repo: root, issue: 88, statePath: statePath, historyPath: historyPath, resultsDir: results}
	if rc := e.stopCommand(); rc == 0 {
		t.Fatal("stop accepted reused/mismatched process identity")
	}
	if !processExists(pid) {
		t.Fatal("stop killed the test process")
	}
}

func TestPublishRemoteDivergenceBecomesInconclusive(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	runGit(t, root, "init", "--bare", bare)
	repo := filepath.Join(root, "work")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "change/91")
	runGit(t, repo, "config", "user.name", "Runtime Test")
	runGit(t, repo, "config", "user.email", "runtime@example.test")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "one")
	candidate := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	runGit(t, repo, "remote", "add", "origin", bare)
	runGit(t, repo, "push", "origin", "HEAD:change/91")

	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "two")
	diverged := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	runGit(t, repo, "push", "--force", "origin", "HEAD:change/91")
	// Restore local Candidate while leaving remote at the diverged commit.
	runGit(t, repo, "reset", "--hard", candidate)

	statePath := filepath.Join(root, "state.json")
	e := &engine{ui: newUI(ColorNever), repo: root, issue: 91, branch: "change/91", worktree: repo, statePath: statePath, originURL: bare}
	state := RuntimeState{
		Version: 4, Status: "COMMITTED_PENDING_PUSH", CandidateHead: candidate,
		CandidateRemoteBefore: "expected-old-remote",
	}
	if rc := e.publishCommittedCandidate(&state); rc == 0 {
		t.Fatal("diverged remote was accepted")
	}
	if state.Status != "PUBLISH_INCONCLUSIVE" {
		t.Fatalf("status=%s", state.Status)
	}
	remote := strings.TrimSpace(runGitOutput(t, repo, "ls-remote", bare, "refs/heads/change/91"))
	if !strings.HasPrefix(remote, diverged+"\t") {
		t.Fatalf("remote was mutated: %q", remote)
	}
}

func TestValidateWorktreeDirtyFailsClosed(t *testing.T) {
	repo := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &engine{repo: repo, worktree: repo, branch: "main", workerHome: filepath.Join(repo, ".cddm-worker-home")}
	if err := e.validateWorktree(true); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty worktree accepted: %v", err)
	}
}

func TestRuntimeStateVersion4ReadsAdditiveIdentityFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := `{"version":4,"issue":5,"status":"RUNNING","active_pid":123,"active_pgid":123,"active_pid_start_ticks":"456"}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 4 || state.ActivePID == nil || *state.ActivePID != 123 || state.ActivePGID == nil || *state.ActivePGID != 123 || state.ActivePIDStartTicks != "456" {
		t.Fatalf("state=%#v", state)
	}
}

func TestStructuredResultRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	writeResult(t, path, "BLOCKED")
	result, err := validateWorkerResult(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(data), `"status":"BLOCKED"`) {
		t.Fatalf("round trip: %s err=%v", data, err)
	}
}
