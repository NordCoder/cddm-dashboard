package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (e *engine) dispatchResult(state *RuntimeState, resultPath, v2Log string) int {
	result, err := validateWorkerResult(resultPath)
	if err != nil {
		e.ui.errorf("validate Worker result: %v", err)
		return 13
	}
	if state.LastResult == resultPath {
		if state.LastResultRC != nil {
			return *state.LastResultRC
		}
		return 0
	}

	switch result.Status {
	case "CONTINUE":
		state.Status = "CONTINUE"
		e.setResultDisposition(state, resultPath, 0)
		if err := saveStateAtomic(e.statePath, *state); err != nil {
			return 1
		}
		e.printResult(result)
		return 0
	case "BLOCKED":
		state.Status = "BLOCKER_PENDING_GITHUB"
		if err := saveStateAtomic(e.statePath, *state); err != nil {
			return 1
		}
		if err := e.persistBlocker(resultPath, result); err != nil {
			e.ui.errorf("persist blocker: %v", err)
			return 1
		}
		state.Status = "BLOCKED"
		e.setResultDisposition(state, resultPath, 0)
		if err := saveStateAtomic(e.statePath, *state); err != nil {
			return 1
		}
		e.printResult(result)
		return 0
	case "NO_OP":
		if len(gitStatus(e.worktree)) != 0 {
			state.Status = "INVALID_NO_OP"
			e.setResultDisposition(state, resultPath, 14)
			_ = saveStateAtomic(e.statePath, *state)
			e.ui.errorf("NO_OP returned with file changes")
			return 14
		}
		state.Status = "NO_OP"
		e.setResultDisposition(state, resultPath, 0)
		if err := saveStateAtomic(e.statePath, *state); err != nil {
			return 1
		}
		e.printResult(result)
		return 0
	case "CANDIDATE_READY":
		if state.CandidateResult == resultPath {
			e.setResultDisposition(state, resultPath, 0)
			if err := saveStateAtomic(e.statePath, *state); err != nil {
				return 1
			}
			return e.reconcilePendingCandidate(state)
		}
		return e.commitAndPublishCandidate(state, resultPath, v2Log)
	default:
		return 13
	}
}

func (e *engine) setResultDisposition(state *RuntimeState, result string, rc int) {
	state.LastResult = result
	state.LastResultRC = ptr(rc)
}

func (e *engine) printResult(result workerResult) {
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func (e *engine) runCandidateV2(logPath string) error {
	for _, command := range []string{"gofmt", "go", "npm", "docker"} {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("missing Candidate verifier: %s", command)
		}
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	log, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer log.Close()

	if err := e.v2Gofmt(log); err != nil {
		return err
	}
	phases := []struct {
		name string
		dir  string
		cmd  string
		args []string
	}{
		{"Go tests", filepath.Join(e.worktree, "backend"), "go", []string{"test", "./..."}},
		{"Go race", filepath.Join(e.worktree, "backend"), "go", []string{"test", "-race", "./..."}},
		{"npm install", filepath.Join(e.worktree, "web"), "npm", []string{"ci"}},
		{"npm test", filepath.Join(e.worktree, "web"), "npm", []string{"test"}},
		{"npm build", filepath.Join(e.worktree, "web"), "npm", []string{"run", "build"}},
		{"Docker Compose config", e.worktree, "docker", []string{"compose", "config", "--quiet"}},
	}
	for _, phase := range phases {
		if err := e.runV2Phase(log, phase.name, phase.dir, phase.cmd, phase.args...); err != nil {
			return err
		}
	}
	return nil
}

func (e *engine) v2Gofmt(log *os.File) error {
	var files []string
	root := filepath.Join(e.worktree, "backend")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	start := time.Now()
	fmt.Fprintf(os.Stdout, "%s %s\n", e.ui.style(os.Stdout, ansiYellow, "› RUN"), "gofmt")
	args := append([]string{"-l"}, files...)
	var output bytes.Buffer
	cmd := exec.Command("gofmt", args...)
	cmd.Dir = root
	cmd.Stdout = io.MultiWriter(log, &output)
	cmd.Stderr = io.MultiWriter(log, &output)
	runErr := cmd.Run()
	if runErr == nil && strings.TrimSpace(output.String()) != "" {
		runErr = errors.New("unformatted Go files")
	}
	if runErr != nil {
		fmt.Fprintf(os.Stdout, "%s %-28s %s\n", e.ui.style(os.Stdout, ansiRed, "✗ FAIL"), "gofmt", humanDuration(time.Since(start)))
		e.printV2Tail(output.String())
		return fmt.Errorf("gofmt: %w", runErr)
	}
	fmt.Fprintf(os.Stdout, "%s %-28s %s\n", e.ui.style(os.Stdout, ansiGreen, "✓ PASS"), "gofmt", humanDuration(time.Since(start)))
	return nil
}

type tailBuffer struct {
	limit int
	data  []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.data = append(t.data, p...)
	if len(t.data) > t.limit {
		t.data = append([]byte(nil), t.data[len(t.data)-t.limit:]...)
	}
	return len(p), nil
}

func (e *engine) runV2Phase(log *os.File, phase, dir, command string, args ...string) error {
	start := time.Now()
	fmt.Fprintf(os.Stdout, "%s %s\n", e.ui.style(os.Stdout, ansiYellow, "› RUN"), phase)
	tail := &tailBuffer{limit: 12000}
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Stdout = io.MultiWriter(log, tail)
	cmd.Stderr = io.MultiWriter(log, tail)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stdout, "%s %-28s %s\n", e.ui.style(os.Stdout, ansiRed, "✗ FAIL"), phase, humanDuration(time.Since(start)))
		e.printV2Tail(string(tail.data))
		return fmt.Errorf("%s: %w", phase, err)
	}
	fmt.Fprintf(os.Stdout, "%s %-28s %s\n", e.ui.style(os.Stdout, ansiGreen, "✓ PASS"), phase, humanDuration(time.Since(start)))
	return nil
}

func (e *engine) printV2Tail(text string) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	for _, line := range lines {
		fmt.Fprintf(os.Stdout, "  %s\n", e.ui.style(os.Stdout, ansiDim, line))
	}
}

func (e *engine) commitAndPublishCandidate(state *RuntimeState, resultPath, v2Log string) int {
	if len(gitStatus(e.worktree)) == 0 {
		state.Status = "INVALID_CANDIDATE_READY"
		e.setResultDisposition(state, resultPath, 1)
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("CANDIDATE_READY produced no file changes; use NO_OP")
		return 1
	}
	if err := e.runCandidateV2(v2Log); err != nil {
		state.Status = "V2_FAILED"
		e.setResultDisposition(state, resultPath, 4)
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("Host V2 failed; no Candidate published")
		return 4
	}
	if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "diff", "--check"); err != nil {
		e.ui.errorf("working diff check: %v", err)
		return 1
	}
	if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "add", "-A"); err != nil {
		return 1
	}
	if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "diff", "--cached", "--check"); err != nil {
		e.ui.errorf("staged diff check: %v", err)
		return 1
	}
	parentOut, err := runOutput(e.worktree, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return 1
	}
	treeOut, err := runOutput(e.worktree, nil, "git", "write-tree")
	if err != nil {
		return 1
	}
	parent := strings.TrimSpace(parentOut)
	tree := strings.TrimSpace(treeOut)
	commitOut, err := runOutputWithStdin(e.worktree, fmt.Sprintf("Implement Issue #%d\n", e.issue), "git", "commit-tree", tree, "-p", parent)
	if err != nil {
		e.ui.errorf("create Candidate commit: %v", err)
		return 1
	}
	head := strings.TrimSpace(commitOut)
	remoteBefore, _ := e.remoteBranchHead()
	state.Status = "COMMIT_PREPARED"
	state.CandidateResult = resultPath
	state.CandidateHead = head
	state.CandidateParent = parent
	state.CandidateRemoteBefore = remoteBefore
	if err := saveStateAtomic(e.statePath, *state); err != nil {
		return 1
	}
	e.setResultDisposition(state, resultPath, 0)
	if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "reset", "--hard", head); err != nil {
		return 1
	}
	state.Status = "COMMITTED_PENDING_PUSH"
	if err := saveStateAtomic(e.statePath, *state); err != nil {
		return 1
	}
	return e.publishCommittedCandidate(state)
}

func runOutputWithStdin(dir, stdin, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (e *engine) remoteBranchHead() (string, error) {
	out, err := runOutput(e.repo, nil, "git", "ls-remote", e.originURL, "refs/heads/"+e.branch)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

func (e *engine) publishCommittedCandidate(state *RuntimeState) int {
	head := state.CandidateHead
	expected := state.CandidateRemoteBefore
	if head == "" {
		e.ui.errorf("no committed Candidate to publish")
		return 1
	}
	localOut, err := runOutput(e.worktree, nil, "git", "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(localOut) != head {
		state.Status = "PUBLISH_INCONCLUSIVE"
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("local Candidate Head changed")
		return 1
	}
	remote, err := e.remoteBranchHead()
	if err == nil {
		if remote == head {
			state.Status = "PUSHED_PENDING_GITHUB"
			_ = saveStateAtomic(e.statePath, *state)
			return e.finalizePushedCandidate(state)
		}
		if remote != expected {
			state.Status = "PUBLISH_INCONCLUSIVE"
			_ = saveStateAtomic(e.statePath, *state)
			e.ui.errorf("remote Change ref diverged before publish: expected=%s actual=%s", defaultString(expected, "missing"), defaultString(remote, "missing"))
			return 1
		}
	}
	lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", e.branch, expected)
	pushErr := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "push", lease, e.originURL, "HEAD:refs/heads/"+e.branch)
	remoteAfter, confirmErr := e.remoteBranchHead()
	if confirmErr == nil {
		if remoteAfter == head {
			state.Status = "PUSHED_PENDING_GITHUB"
			_ = saveStateAtomic(e.statePath, *state)
			return e.finalizePushedCandidate(state)
		}
		if remoteAfter != expected {
			state.Status = "PUBLISH_INCONCLUSIVE"
			_ = saveStateAtomic(e.statePath, *state)
			e.ui.errorf("remote Change ref diverged during publish: expected=%s actual=%s", defaultString(expected, "missing"), defaultString(remoteAfter, "missing"))
			return 1
		}
		state.Status = "COMMITTED_PENDING_PUSH"
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("Candidate push not accepted yet; exact commit preserved (err=%v)", pushErr)
		return 1
	}
	state.Status = "PUBLISH_CONFIRMATION_PENDING"
	_ = saveStateAtomic(e.statePath, *state)
	e.ui.errorf("Candidate push outcome could not be confirmed; exact commit preserved")
	return 1
}

func (e *engine) finalizePushedCandidate(state *RuntimeState) int {
	head := state.CandidateHead
	remote, err := e.remoteBranchHead()
	if err != nil {
		state.Status = "PUBLISH_CONFIRMATION_PENDING"
		_ = saveStateAtomic(e.statePath, *state)
		return 1
	}
	if remote != head {
		state.Status = "PUBLISH_INCONCLUSIVE"
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("remote Candidate mismatch")
		return 1
	}
	pr, err := e.ensurePR()
	if err != nil {
		state.Status = "PUSHED_PENDING_GITHUB"
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("ensure PR: %v", err)
		return 1
	}
	if err := e.verifyPRHead(pr, head); err != nil {
		state.Status = "PUSHED_PENDING_GITHUB"
		state.PR = ptr(pr)
		_ = saveStateAtomic(e.statePath, *state)
		e.ui.errorf("verify PR Head: %v", err)
		return 1
	}
	state.Status = "CANDIDATE"
	state.PR = ptr(pr)
	if err := saveStateAtomic(e.statePath, *state); err != nil {
		return 1
	}
	payload := map[string]any{"host_status": "CANDIDATE_PUBLISHED", "head": head, "pr": pr}
	data, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Println(string(data))
	return 0
}

func (e *engine) reconcilePendingCandidate(state *RuntimeState) int {
	switch state.Status {
	case "COMMIT_PREPARED":
		return e.reconcilePreparedCandidate(state)
	case "COMMITTED_PENDING_PUSH", "PUBLISH_CONFIRMATION_PENDING":
		return e.publishCommittedCandidate(state)
	case "PUSHED_PENDING_GITHUB":
		return e.finalizePushedCandidate(state)
	case "PUBLISH_INCONCLUSIVE":
		e.ui.errorf("Candidate publication is inconclusive; Web Lead reconciliation required")
		return 1
	default:
		return 0
	}
}

func (e *engine) reconcilePreparedCandidate(state *RuntimeState) int {
	if state.CandidateHead == "" || state.CandidateParent == "" {
		state.Status = "PUBLISH_INCONCLUSIVE"
		_ = saveStateAtomic(e.statePath, *state)
		return 1
	}
	if err := runCommand(e.worktree, nil, nil, io.Discard, io.Discard, "git", "cat-file", "-e", state.CandidateHead+"^{commit}"); err != nil {
		state.Status = "PUBLISH_INCONCLUSIVE"
		_ = saveStateAtomic(e.statePath, *state)
		return 1
	}
	localOut, err := runOutput(e.worktree, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return 1
	}
	local := strings.TrimSpace(localOut)
	if local == state.CandidateParent {
		if err := runCommand(e.worktree, nil, nil, io.Discard, os.Stderr, "git", "reset", "--hard", state.CandidateHead); err != nil {
			return 1
		}
	} else if local != state.CandidateHead {
		state.Status = "PUBLISH_INCONCLUSIVE"
		_ = saveStateAtomic(e.statePath, *state)
		return 1
	}
	state.Status = "COMMITTED_PENDING_PUSH"
	_ = saveStateAtomic(e.statePath, *state)
	return e.publishCommittedCandidate(state)
}

func (e *engine) findPRForBranch() (int, error) {
	out, err := runOutput(e.repo, nil, "gh", "pr", "list", "--repo", repoSlug, "--head", e.branch, "--state", "open", "--json", "number")
	if err != nil {
		return 0, err
	}
	var rows []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Number, nil
}

func (e *engine) ensurePR() (int, error) {
	if pr, err := e.findPRForBranch(); err != nil || pr != 0 {
		return pr, err
	}
	issueOut, err := runOutput(e.repo, nil, "gh", "issue", "view", strconvI(e.issue), "--repo", repoSlug, "--json", "title")
	if err != nil {
		return 0, err
	}
	var issue struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(issueOut), &issue); err != nil {
		return 0, err
	}
	body := fmt.Sprintf("Closes #%d\n\nCanonical Change Contract: `%s`\n\nCDDM WebLead 3.0: Web Lead owns WHAT + HARD HOW + QA; implementation uses one persistent Codex Change session unless explicitly rotated.", e.issue, e.contract)
	if err := runCommand(e.repo, nil, nil, io.Discard, os.Stderr, "gh", "pr", "create", "--repo", repoSlug, "--draft", "--base", "main", "--head", e.branch, "--title", issue.Title, "--body", body); err != nil {
		return 0, err
	}
	pr, err := e.findPRForBranch()
	if err != nil || pr == 0 {
		return 0, errors.New("unable to resolve/create PR")
	}
	return pr, nil
}

func (e *engine) verifyPRHead(pr int, expected string) error {
	out, err := runOutput(e.repo, nil, "gh", "pr", "view", strconvI(pr), "--repo", repoSlug, "--json", "headRefOid")
	if err != nil {
		return err
	}
	var v struct {
		Head string `json:"headRefOid"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return err
	}
	if v.Head != expected {
		return fmt.Errorf("PR Head mismatch: expected=%s actual=%s", expected, v.Head)
	}
	return nil
}

func (e *engine) persistBlocker(resultPath string, result workerResult) error {
	marker := "<!-- cddm-blocker-result:" + filepath.Base(resultPath) + " -->"
	body := fmt.Sprintf("%s\n\n## CDDM WebLead 3.0 Blocker\n\nSUMMARY: %s\nBLOCKER: %s", marker, result.Summary, result.Blocker)
	if present, err := e.commentMarkerPresent(e.issue, marker); err != nil {
		return err
	} else if present {
		return nil
	}
	target := e.issue
	if pr, err := e.findPRForBranch(); err != nil {
		return err
	} else if pr != 0 {
		if present, err := e.commentMarkerPresent(pr, marker); err != nil {
			return err
		} else if present {
			return nil
		}
		target = pr
	}
	return runCommand(e.repo, nil, nil, io.Discard, os.Stderr, "gh", "api", fmt.Sprintf("repos/%s/issues/%d/comments", repoSlug, target), "-f", "body="+body)
}

func (e *engine) commentMarkerPresent(target int, marker string) (bool, error) {
	out, err := runOutput(e.repo, nil, "gh", "api", "--paginate", fmt.Sprintf("repos/%s/issues/%d/comments", repoSlug, target), "--jq", ".[].body")
	if err != nil {
		return false, err
	}
	return strings.Contains(out, marker), nil
}
