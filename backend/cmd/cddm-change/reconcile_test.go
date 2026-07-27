package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileOntoPreservesCandidatePatch(t *testing.T) {
	e, state, parent, candidate, newBase, bare := prepareReconcileRepo(t, 200)
	if state.CandidateParent != parent || state.CandidateHead != candidate {
		t.Fatalf("bad test state: %#v", state)
	}

	if rc := e.reconcileOnto(&state, newBase); rc != 0 {
		t.Fatalf("reconcile rc=%d state=%#v", rc, state)
	}
	if state.Status != "RECONCILING" || state.ReconcileFromHead != candidate || state.ReconcileBase != newBase || state.ReconcilePatch == "" {
		t.Fatalf("reconcile state=%#v", state)
	}
	if state.CandidateHead != "" || state.CandidateParent != "" || state.CandidateResult != "" {
		t.Fatalf("candidate identity was not cleared: %#v", state)
	}
	local := strings.TrimSpace(runGitOutput(t, e.worktree, "rev-parse", "HEAD"))
	if local != newBase {
		t.Fatalf("local head=%s want=%s", local, newBase)
	}
	feature, err := os.ReadFile(filepath.Join(e.worktree, "feature.txt"))
	if err != nil || string(feature) != "candidate\n" {
		t.Fatalf("candidate patch not preserved: %q err=%v", feature, err)
	}
	if len(gitStatus(e.worktree)) == 0 {
		t.Fatal("reconciled worktree is unexpectedly clean")
	}
	remote := strings.TrimSpace(runGitOutput(t, e.worktree, "ls-remote", bare, "refs/heads/change/200"))
	if !strings.HasPrefix(remote, newBase+"\t") {
		t.Fatalf("remote=%q want head=%s", remote, newBase)
	}
	if info, err := os.Stat(state.ReconcilePatch); err != nil || info.Size() == 0 {
		t.Fatalf("reconcile patch missing: info=%v err=%v", info, err)
	}
}

func TestReconcileOntoFailsClosedOnCandidateIdentityMismatch(t *testing.T) {
	e, state, parent, candidate, newBase, bare := prepareReconcileRepo(t, 201)
	if err := runCommand(e.worktree, nil, nil, os.Stdout, os.Stderr, "git", "reset", "--hard", parent); err != nil {
		t.Fatal(err)
	}

	if rc := e.reconcileOnto(&state, newBase); rc == 0 {
		t.Fatal("reconcile accepted mismatched local Candidate identity")
	}
	if state.CandidateHead != candidate || state.CandidateParent != parent || state.Status != "CANDIDATE" {
		t.Fatalf("state mutated after rejected reconcile: %#v", state)
	}
	local := strings.TrimSpace(runGitOutput(t, e.worktree, "rev-parse", "HEAD"))
	if local != parent {
		t.Fatalf("local head changed: %s", local)
	}
	remote := strings.TrimSpace(runGitOutput(t, e.worktree, "ls-remote", bare, "refs/heads/change/201"))
	if !strings.HasPrefix(remote, candidate+"\t") {
		t.Fatalf("remote mutated: %q", remote)
	}
}

func prepareReconcileRepo(t *testing.T, issue int) (*engine, RuntimeState, string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	runGit(t, root, "init", "--bare", bare)
	repo := filepath.Join(root, "work")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Runtime Test")
	runGit(t, repo, "config", "user.email", "runtime@example.test")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "base")
	parent := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "main.txt"), []byte("new main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "new main")
	newBase := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	runGit(t, repo, "remote", "add", "origin", bare)
	branch := "change/" + strconvI(issue)
	runGit(t, repo, "checkout", "-b", branch, parent)
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "candidate")
	candidate := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	runGit(t, repo, "push", "origin", "HEAD:refs/heads/"+branch)

	statePath := filepath.Join(root, "runtime", "state.json")
	state := RuntimeState{
		Version: 4, Issue: issue, Branch: branch, Worktree: repo, Status: "CANDIDATE", ThreadGeneration: 1,
		CandidateHead: candidate, CandidateParent: parent,
	}
	if err := saveStateAtomic(statePath, state); err != nil {
		t.Fatal(err)
	}
	e := &engine{
		ui: newUI(ColorNever), repo: repo, issue: issue, branch: branch, worktree: repo,
		statePath: statePath, resultsDir: filepath.Join(root, "results"), originURL: bare,
	}
	return e, state, parent, candidate, newBase, bare
}
