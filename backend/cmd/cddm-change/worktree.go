package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (e *engine) ensureWorkerHome() error {
	for _, path := range []string{
		filepath.Join(e.workerHome, ".config", "gh"),
		filepath.Join(e.workerHome, ".cache"),
		filepath.Join(e.workerHome, ".local", "share"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	excludePath, err := runOutput(e.worktree, nil, "git", "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	excludePath = strings.TrimSpace(excludePath)
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(e.worktree, excludePath)
	}
	data, _ := os.ReadFile(excludePath)
	if !linePresent(string(data), ".cddm-worker-home/") {
		f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, writeErr := io.WriteString(f, ".cddm-worker-home/\n")
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func linePresent(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func (e *engine) expectedInitialHead() (string, error) {
	if refExists(e.repo, "refs/remotes/origin/"+e.branch) {
		out, err := runOutput(e.repo, nil, "git", "rev-parse", "origin/"+e.branch)
		return strings.TrimSpace(out), err
	}
	out, err := runOutput(e.repo, nil, "git", "rev-parse", "origin/main")
	return strings.TrimSpace(out), err
}

func refExists(dir, ref string) bool {
	return runCommand(dir, nil, nil, io.Discard, io.Discard, "git", "show-ref", "--verify", "--quiet", ref) == nil
}

func (e *engine) validateWorktree(cleanRequired bool) error {
	if info, err := os.Stat(e.worktree); err != nil || !info.IsDir() {
		return errors.New("persistent worktree is missing")
	}
	root, err := runOutput(e.worktree, nil, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	actualRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return err
	}
	expectedRoot, err := filepath.Abs(e.worktree)
	if err != nil {
		return err
	}
	if actualRoot != expectedRoot {
		return fmt.Errorf("unexpected worktree root: %s", actualRoot)
	}
	branch, err := runOutput(e.worktree, nil, "git", "branch", "--show-current")
	if err != nil {
		return err
	}
	if strings.TrimSpace(branch) != e.branch {
		return fmt.Errorf("worktree is on %q, expected %q", strings.TrimSpace(branch), e.branch)
	}
	if cleanRequired {
		status, err := runOutput(e.worktree, nil, "git", "status", "--porcelain")
		if err != nil {
			return err
		}
		if strings.TrimSpace(status) != "" {
			return errors.New("worktree is dirty")
		}
	}
	return e.ensureWorkerHome()
}

func (e *engine) createOrAttachWorktree() error {
	if info, err := os.Stat(e.worktree); err == nil && info.IsDir() {
		if err := e.validateWorktree(true); err != nil {
			return err
		}
		expected, err := e.expectedInitialHead()
		if err != nil {
			return err
		}
		actual, err := runOutput(e.worktree, nil, "git", "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		if strings.TrimSpace(actual) != expected {
			return fmt.Errorf("orphan worktree Head is not canonical: expected=%s actual=%s", expected, strings.TrimSpace(actual))
		}
		return nil
	}

	if refExists(e.repo, "refs/remotes/origin/"+e.branch) {
		remote, err := runOutput(e.repo, nil, "git", "rev-parse", "origin/"+e.branch)
		if err != nil {
			return err
		}
		remote = strings.TrimSpace(remote)
		if refExists(e.repo, "refs/heads/"+e.branch) {
			local, err := runOutput(e.repo, nil, "git", "rev-parse", e.branch)
			if err != nil {
				return err
			}
			if strings.TrimSpace(local) != remote {
				return fmt.Errorf("local %s diverges from origin/%s", e.branch, e.branch)
			}
		} else if err := runCommand(e.repo, nil, nil, io.Discard, os.Stderr, "git", "branch", "--track", e.branch, "origin/"+e.branch); err != nil {
			return err
		}
		if err := runCommand(e.repo, nil, nil, io.Discard, os.Stderr, "git", "worktree", "add", e.worktree, e.branch); err != nil {
			return err
		}
	} else if refExists(e.repo, "refs/heads/"+e.branch) {
		local, err := runOutput(e.repo, nil, "git", "rev-parse", e.branch)
		if err != nil {
			return err
		}
		main, err := runOutput(e.repo, nil, "git", "rev-parse", "origin/main")
		if err != nil {
			return err
		}
		if strings.TrimSpace(local) != strings.TrimSpace(main) {
			return fmt.Errorf("unpublished local %s is not the canonical initial Head", e.branch)
		}
		if err := runCommand(e.repo, nil, nil, io.Discard, os.Stderr, "git", "worktree", "add", e.worktree, e.branch); err != nil {
			return err
		}
	} else {
		if err := runCommand(e.repo, nil, nil, io.Discard, os.Stderr, "git", "worktree", "add", "-b", e.branch, e.worktree, "origin/main"); err != nil {
			return err
		}
	}
	if err := e.ensureWorkerHome(); err != nil {
		return err
	}
	status, err := runOutput(e.worktree, nil, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("Change worktree is unexpectedly dirty")
	}
	return nil
}

func (e *engine) repairPrethreadFailure() error {
	state, err := loadState(e.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if state.ThreadID != "" || state.Status != "START_FAILED_NO_THREAD" || state.ActiveMode != "start" {
		return nil
	}
	if state.ActivePID != nil && processExists(*state.ActivePID) {
		if _, owned := stateOwnsProcess(state); owned {
			return fmt.Errorf("prior start turn is still alive (pid=%d)", *state.ActivePID)
		}
		return fmt.Errorf("persisted active PID %d is alive but ownership cannot be proven", *state.ActivePID)
	}
	if threadFromEvents(state.ActiveEvents) != "" {
		return errors.New("failed-start events contain a thread.started identity")
	}
	if info, statErr := os.Stat(e.worktree); statErr == nil && info.IsDir() {
		status, statusErr := runOutput(e.worktree, nil, "git", "status", "--porcelain")
		if statusErr != nil {
			return statusErr
		}
		if strings.TrimSpace(status) != "" {
			return errors.New("failed-start worktree is dirty")
		}
	}
	archiveDir := filepath.Join(filepath.Dir(e.statePath), "archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}
	archive := filepath.Join(archiveDir, fmt.Sprintf("issue-%d-prethread-%s.json", e.issue, strings.ReplaceAll(utcNow(), ":", "")))
	if err := os.Rename(e.statePath, archive); err != nil {
		return err
	}
	e.ui.warnf(os.Stderr, "recovered pre-thread failed start; archived stale runtime state")
	return nil
}

func (e *engine) alignCleanPrethreadOrphan() error {
	if _, err := os.Stat(e.statePath); err == nil {
		return nil
	}
	info, err := os.Stat(e.worktree)
	if err != nil || !info.IsDir() {
		return nil
	}
	if err := e.validateWorktree(true); err != nil {
		return err
	}
	expected, err := e.expectedInitialHead()
	if err != nil {
		return err
	}
	actual, err := runOutput(e.worktree, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	actual = strings.TrimSpace(actual)
	if actual == expected {
		return nil
	}
	if err := runCommand(e.worktree, nil, nil, io.Discard, io.Discard, "git", "merge-base", "--is-ancestor", actual, expected); err != nil {
		return fmt.Errorf("orphan Head has unique/divergent history (actual=%s expected=%s)", actual, expected)
	}
	if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "reset", "--hard", expected); err != nil {
		return err
	}
	e.ui.warnf(os.Stderr, "realigned clean pre-thread orphan worktree to %s", expected)
	return nil
}
