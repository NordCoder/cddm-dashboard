package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexCommandWritesOutputLastMessage(t *testing.T) {
	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".codex", "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".codex", "schemas", "change-turn-result.json"), []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := t.TempDir()
	writeTool(t, fake, "codex", `
result=""
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "--output-last-message" ]]; then result="$2"; shift 2; else shift; fi
done
test -n "$result"
printf '{"status":"CONTINUE","summary":"ok","verify":"ok","blocker":"none"}\n' > "$result"
printf '{"type":"thread.started","thread_id":"thread-direct"}\n'
`)
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := &engine{repo: repo, worktree: repo, workerHome: filepath.Join(repo, ".worker")}
	result := filepath.Join(t.TempDir(), "result.json")
	cmd, err := e.codexCommand("start", "", "luna", "medium", result)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = strings.NewReader("prompt")
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(result)
	if err != nil || info.Size() == 0 {
		t.Fatalf("result missing after direct command: info=%v err=%v", info, err)
	}
	if _, err := validateWorkerResult(result); err != nil {
		t.Fatalf("result invalid: %v", err)
	}
}

func TestRunCommandPushesLocalBareRemote(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	runGit(t, root, "init", "--bare", bare)
	repo := filepath.Join(root, "work")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "change/55")
	runGit(t, repo, "config", "user.name", "Runtime Test")
	runGit(t, repo, "config", "user.email", "runtime@example.test")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "one")
	runGit(t, repo, "push", bare, "HEAD:change/55")
	base := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "two")
	head := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if head == base {
		t.Fatal("test commit did not advance")
	}
	if err := runCommand(repo, nil, nil, io.Discard, os.Stderr, "git", "push", bare, "HEAD:refs/heads/change/55"); err != nil {
		t.Fatalf("runCommand push failed: %v", err)
	}
	remote := strings.TrimSpace(runGitOutput(t, repo, "ls-remote", bare, "refs/heads/change/55"))
	if !strings.HasPrefix(remote, head+"\t") {
		t.Fatalf("remote=%q head=%s", remote, head)
	}
}
