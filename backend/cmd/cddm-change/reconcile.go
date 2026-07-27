package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (e *engine) reconcileCommand() int {
	if err := e.preflight(false, true); err != nil {
		e.ui.errorf("reconcile preflight: %v", err)
		return 1
	}
	state, err := loadState(e.statePath)
	if err != nil {
		e.ui.errorf("no runtime state for Issue #%d", e.issue)
		return 1
	}
	if state.ActivePID != nil || state.ActiveMode != "" {
		e.ui.errorf("Issue #%d has an active or unreconciled turn; recover it before base reconciliation", e.issue)
		return 1
	}
	if err := e.validateWorktree(true); err != nil {
		e.ui.errorf("worktree must be clean before base reconciliation: %v", err)
		return 1
	}
	newBaseOut, err := runOutput(e.repo, nil, "git", "rev-parse", "origin/main")
	if err != nil {
		return 1
	}
	return e.reconcileOnto(&state, strings.TrimSpace(newBaseOut))
}

func (e *engine) reconcileOnto(state *RuntimeState, newBase string) int {
	localOut, err := runOutput(e.worktree, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return 1
	}
	localHead := strings.TrimSpace(localOut)
	remoteHead, err := e.remoteBranchHead()
	if err != nil || remoteHead == "" {
		e.ui.errorf("remote Change branch is missing: origin/%s", e.branch)
		return 1
	}
	candidateHead := state.CandidateHead
	candidateParent := state.CandidateParent
	if candidateHead == "" || candidateParent == "" {
		e.ui.errorf("Issue #%d has no published Candidate identity to reconcile", e.issue)
		return 1
	}
	if localHead != candidateHead || remoteHead != candidateHead {
		e.ui.errorf("Candidate identity mismatch before reconcile: state=%s local=%s remote=%s", candidateHead, localHead, remoteHead)
		return 1
	}
	if candidateHead == newBase {
		fmt.Printf("Issue #%d Candidate is already based at current main.\n", e.issue)
		return 0
	}
	if err := runCommand(e.worktree, nil, nil, io.Discard, io.Discard, "git", "cat-file", "-e", candidateParent+"^{commit}"); err != nil {
		e.ui.errorf("Candidate parent object is missing: %s", candidateParent)
		return 1
	}

	patchDir := filepath.Join(filepath.Dir(e.statePath), "reconcile")
	if err := os.MkdirAll(patchDir, 0o755); err != nil {
		return 1
	}
	patchPath := filepath.Join(patchDir, fmt.Sprintf("issue-%d-%s-onto-%s.patch", e.issue, shortSHA(candidateHead), shortSHA(newBase)))
	patchFile, err := os.Create(patchPath)
	if err != nil {
		e.ui.errorf("create reconcile patch: %v", err)
		return 1
	}
	cmd := exec.Command("git", "diff", "--binary", candidateParent, candidateHead)
	cmd.Dir = e.worktree
	cmd.Stdout = patchFile
	cmd.Stderr = os.Stderr
	diffErr := cmd.Run()
	closeErr := patchFile.Close()
	if diffErr != nil || closeErr != nil {
		e.ui.errorf("create reconcile patch failed")
		return 1
	}
	info, err := os.Stat(patchPath)
	if err != nil || info.Size() == 0 {
		e.ui.errorf("Candidate patch is empty; refusing reconcile")
		return 1
	}

	if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "reset", "--hard", newBase); err != nil {
		return 1
	}
	applyErr := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "apply", "--3way", "--whitespace=nowarn", patchPath)
	applyRC := 0
	if applyErr != nil {
		applyRC = exitCode(applyErr)
	}
	if applyErr != nil && len(gitStatus(e.worktree)) == 0 {
		_ = runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "reset", "--hard", candidateHead)
		e.ui.errorf("Candidate patch could not be applied and produced no reconcilable worktree changes; restored old Candidate")
		return 1
	}
	if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "reset", "--mixed"); err != nil {
		return 1
	}
	lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", e.branch, remoteHead)
	if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "push", lease, e.originURL, "HEAD:refs/heads/"+e.branch); err != nil {
		_ = runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "reset", "--hard", candidateHead)
		e.ui.errorf("failed to move remote Change branch to the new integration base; restored old local Candidate")
		return 1
	}

	state.Status = "RECONCILING"
	state.ReconcileFromHead = candidateHead
	state.ReconcileBase = newBase
	state.ReconcilePatch = patchPath
	state.CandidateHead = ""
	state.CandidateParent = ""
	state.CandidateRemoteBefore = ""
	state.CandidateResult = ""
	if err := saveStateAtomic(e.statePath, *state); err != nil {
		e.ui.errorf("persist reconciliation state: %v", err)
		return 1
	}
	fmt.Printf("Prepared Issue #%d reconciliation onto %s (previous Candidate %s, apply_rc=%d).\n", e.issue, newBase, candidateHead, applyRC)
	fmt.Println("Worktree changes are preserved for the existing persistent Change thread.")
	for _, line := range gitStatus(e.worktree) {
		fmt.Println(line)
	}
	return 0
}

func shortSHA(v string) string {
	if len(v) <= 12 {
		return v
	}
	return v[:12]
}
