package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadIntFileAllowsRealPIDValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pid")
	if err := os.WriteFile(path, []byte("25444\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, err := readIntFile(path)
	if err != nil || pid != 25444 {
		t.Fatalf("pid=%d err=%v", pid, err)
	}
	if _, err := readExitStatus(path); err == nil {
		t.Fatal("large PID value was accepted as an exit status")
	}
}

func TestRecoverReusedPIDUsesDurableEvidenceWithoutSignal(t *testing.T) {
	root := t.TempDir()
	statePath, historyPath, resultsDir := statePaths(root, 123)
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	events := filepath.Join(resultsDir, "events.jsonl")
	result := filepath.Join(resultsDir, "result.json")
	exitStatus := filepath.Join(resultsDir, "exit-status")
	if err := os.WriteFile(events, []byte("{\"type\":\"thread.started\",\"thread_id\":\"thread-r\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result, []byte("must-not-be-dispatched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exitStatus, []byte("143\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pid := os.Getpid()
	identity, err := readProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	pgid := identity.PGID
	state := RuntimeState{
		Version: 4, Issue: 123, ThreadID: "thread-r", Status: "RUNNING", ThreadGeneration: 1,
		ActivePID: &pid, ActivePGID: &pgid, ActivePIDStartTicks: identity.StartTicks + "-reused",
		ActiveMode: "resume", ActiveEvents: events, ActiveResult: result, ActiveExitStatus: exitStatus,
		ActivePreviousThread: "thread-r", ActiveModel: "luna", ActiveReasoning: "medium",
	}
	if err := saveStateAtomic(statePath, state); err != nil {
		t.Fatal(err)
	}

	e := &engine{ui: newUI(ColorNever), repo: root, issue: 123, statePath: statePath, historyPath: historyPath, resultsDir: resultsDir}
	loaded, err := loadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	recovered, rc := e.recoverActive(&loaded)
	if !recovered || rc != 143 {
		t.Fatalf("recovered=%v rc=%d state=%#v", recovered, rc, loaded)
	}
	if loaded.Status != "TURN_FAILED" || loaded.ActivePID != nil || loaded.ActiveMode != "" {
		t.Fatalf("recovered state=%#v", loaded)
	}
	if !processExists(pid) {
		t.Fatal("recovery signalled an unrelated live process")
	}
}

func TestRepairPrethreadFailureIgnoresReusedPID(t *testing.T) {
	repo := initGitRepo(t)
	statePath, _, resultsDir := statePaths(repo, 124)
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(resultsDir, "prethread-events.jsonl")
	if err := os.WriteFile(events, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	pid := os.Getpid()
	identity, err := readProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	pgid := identity.PGID
	state := RuntimeState{
		Version: 4, Issue: 124, Status: "START_FAILED_NO_THREAD", ThreadGeneration: 1,
		ActivePID: &pid, ActivePGID: &pgid, ActivePIDStartTicks: identity.StartTicks + "-reused",
		ActiveMode: "start", ActiveEvents: events,
	}
	if err := saveStateAtomic(statePath, state); err != nil {
		t.Fatal(err)
	}

	e := &engine{
		ui: newUI(ColorNever), repo: repo, issue: 124, worktree: repo,
		statePath: statePath, workerHome: filepath.Join(repo, ".cddm-worker-home"),
	}
	if err := e.repairPrethreadFailure(); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("stale pre-thread state was not archived: %v", err)
	}
	if !processExists(pid) {
		t.Fatal("pre-thread repair signalled an unrelated live process")
	}
}
