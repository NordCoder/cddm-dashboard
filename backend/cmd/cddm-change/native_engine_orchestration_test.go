package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexStdoutPipeConcurrentWaitPreservesResult(t *testing.T) {
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
printf '{"type":"thread.started","thread_id":"thread-pipe"}\n'
printf '{"status":"CONTINUE","summary":"ok","verify":"ok","blocker":"none"}\n' > "$result"
test -s "$result"
`)
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := &engine{repo: repo, worktree: repo, workerHome: filepath.Join(repo, ".worker")}
	result := filepath.Join(t.TempDir(), "result.json")
	cmd, err := e.codexCommand("start", "", "luna", "medium", result)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	if _, err := io.ReadAll(stdout); err != nil {
		t.Fatal(err)
	}
	if err := <-waitCh; err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(result)
	if err != nil || info.Size() == 0 {
		t.Fatalf("result missing after concurrent Wait: info=%v err=%v", info, err)
	}
}

func TestRunCommandPushWithFakeV2Path(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	runGit(t, root, "init", "--bare", bare)
	repo := filepath.Join(root, "work")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "change/56")
	runGit(t, repo, "config", "user.name", "Runtime Test")
	runGit(t, repo, "config", "user.email", "runtime@example.test")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "one")
	runGit(t, repo, "push", bare, "HEAD:change/56")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "two")
	head := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))

	fake := t.TempDir()
	for _, tool := range []string{"gofmt", "go", "npm", "docker"} {
		writeTool(t, fake, tool, "exit 0")
	}
	writeTool(t, fake, "gh", "exit 0")
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	ssh, sshErr := exec.LookPath("ssh")
	if sshErr != nil {
		t.Logf("ssh unavailable in test PATH: %v", sshErr)
	} else {
		t.Logf("ssh=%s", ssh)
	}

	if err := runCommand(repo, nil, nil, io.Discard, os.Stderr, "git", "push", bare, "HEAD:refs/heads/change/56"); err != nil {
		t.Fatalf("push with fake V2 PATH failed: %v PATH=%s", err, os.Getenv("PATH"))
	}
	remote := strings.TrimSpace(runGitOutput(t, repo, "ls-remote", bare, "refs/heads/change/56"))
	if !strings.HasPrefix(remote, head+"\t") {
		t.Fatalf("remote=%q head=%s", remote, head)
	}
}
