package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const permissionProfile = "cddm-worker"

var repoSlug string

type executionOptions struct {
	ProfileModel     string
	ProfileReasoning string
	Model            string
	Reasoning        string
	CodexProfile     string
}

type engine struct {
	ui          *UI
	repo        string
	issue       int
	branch      string
	worktree    string
	workerHome  string
	statePath   string
	historyPath string
	resultsDir  string
	contract    string
	originURL   string
	repoSlug    string
}

func newEngine(ui *UI, repo string, issue int) (*engine, error) {
	statePath, historyPath, resultsDir := statePaths(repo, issue)
	contract, err := resolveContract(repo, issue)
	if err != nil {
		return nil, err
	}
	originOut, err := runOutput(repo, nil, "git", "remote", "get-url", "origin")
	if err != nil {
		return nil, fmt.Errorf("read origin: %w", err)
	}
	origin := strings.TrimSpace(originOut)
	slug, err := githubRepoSlug(origin)
	if err != nil {
		return nil, err
	}
	repoSlug = slug
	return &engine{
		ui: ui, repo: repo, issue: issue, branch: fmt.Sprintf("change/%d", issue),
		worktree:   filepath.Join(repo, ".worktrees", fmt.Sprintf("issue-%d", issue)),
		workerHome: filepath.Join(repo, ".worktrees", fmt.Sprintf("issue-%d", issue), ".cddm-worker-home"),
		statePath:  statePath, historyPath: historyPath, resultsDir: resultsDir, contract: contract,
		originURL: origin, repoSlug: slug,
	}, nil
}

func commandMutating(ui *UI, repo, command string, args []string) int {
	return commandMutatingWithOptions(ui, repo, command, args, executionOptions{})
}

func commandMutatingWithOptions(ui *UI, repo, command string, args []string, execOpts executionOptions) int {
	if len(args) < 1 {
		ui.errorf("%s requires an Issue number", command)
		return 2
	}
	issue, err := parseIssue(args[0])
	if err != nil {
		ui.errorf("%v", err)
		return 2
	}
	e, err := newEngine(ui, repo, issue)
	if err != nil {
		ui.errorf("initialize runtime: %v", err)
		return 1
	}
	if command == "stop" {
		if len(args) != 1 {
			ui.errorf("usage: cddm stop <issue>")
			return 2
		}
		return e.stopCommand()
	}
	lock, err := acquireIssueLock(repo, issue)
	if err != nil {
		ui.errorf("%v", err)
		return 1
	}
	defer lock.Close()

	switch command {
	case "start":
		if len(args) > 3 {
			ui.errorf("usage: cddm start <issue> [legacy-model] [legacy-reasoning]")
			return 2
		}
		if err := e.selectCodexProfile(execOpts.CodexProfile); err != nil {
			ui.errorf("Codex profile: %v", err)
			return 1
		}
		model, reasoning := "gpt-5.6-terra", "medium"
		if execOpts.ProfileModel != "" {
			model = execOpts.ProfileModel
		}
		if execOpts.ProfileReasoning != "" {
			reasoning = execOpts.ProfileReasoning
		}
		if len(args) >= 2 {
			model = args[1]
		}
		if len(args) >= 3 {
			reasoning = args[2]
		}
		if execOpts.Model != "" {
			model = execOpts.Model
		}
		if execOpts.Reasoning != "" {
			reasoning = execOpts.Reasoning
		}
		return e.start(model, reasoning)
	case "resume", "rotate":
		if len(args) < 2 || len(args) > 4 {
			ui.errorf("usage: cddm %s <issue> <instruction-file|-> [legacy-model] [legacy-reasoning]", command)
			return 2
		}
		instruction, err := readInstruction(args[1])
		if err != nil {
			ui.errorf("read instruction: %v", err)
			return 2
		}
		if strings.TrimSpace(instruction) == "" {
			ui.errorf("%s instruction is empty", command)
			return 2
		}
		return e.resumeOrRotateWithOptions(command, instruction, args[2:], execOpts)
	case "recover":
		if len(args) != 1 {
			ui.errorf("usage: cddm recover <issue>")
			return 2
		}
		return e.recoverCommand()
	case "reconcile":
		if len(args) != 1 {
			ui.errorf("usage: cddm reconcile <issue>")
			return 2
		}
		return e.reconcileCommand()
	default:
		ui.errorf("unsupported mutating command %q", command)
		return 2
	}
}

func resolveContract(repo string, issue int) (string, error) {
	matches, err := filepath.Glob(filepath.Join(repo, ".delivery", "changes", fmt.Sprintf("%d-*.md", issue)))
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple Change Contracts found for Issue #%d", issue)
	}
	if len(matches) == 0 {
		return "none", nil
	}
	rel, err := filepath.Rel(repo, matches[0])
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func (e *engine) preflight(requireCodex bool, syncMain bool) error {
	for _, name := range []string{"git", "gh"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("missing required command: %s", name)
		}
	}
	if requireCodex {
		if _, err := exec.LookPath("codex"); err != nil {
			return errors.New("missing required command: codex")
		}
	}
	branch, err := runOutput(e.repo, nil, "git", "branch", "--show-current")
	if err != nil || strings.TrimSpace(branch) != "main" {
		return errors.New("target repository must be on controlling main checkout")
	}
	status, err := runOutput(e.repo, nil, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("controlling main must be clean")
	}
	origin, err := runOutput(e.repo, nil, "git", "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	e.originURL = strings.TrimSpace(origin)
	slug, err := githubRepoSlug(e.originURL)
	if err != nil {
		return err
	}
	if slug != e.repoSlug {
		return fmt.Errorf("repository origin changed during command: expected=%s actual=%s", e.repoSlug, slug)
	}
	repoSlug = slug
	if _, err := runOutput(e.repo, nil, "gh", "auth", "status"); err != nil {
		return errors.New("GitHub CLI is not authenticated")
	}
	if requireCodex {
		if _, err := runOutput(e.repo, nil, "codex", "login", "status"); err != nil {
			return errors.New("Codex CLI is not authenticated")
		}
	}
	for _, key := range []string{"user.name", "user.email"} {
		if value, err := runOutput(e.repo, nil, "git", "config", key); err != nil || strings.TrimSpace(value) == "" {
			return fmt.Errorf("Git %s is not configured", key)
		}
	}
	if syncMain {
		if err := runCommand(e.repo, nil, nil, io.Discard, os.Stderr, "git", "fetch", "--prune", "origin", "--quiet"); err != nil {
			return err
		}
		if err := runCommand(e.repo, nil, nil, io.Discard, os.Stderr, "git", "merge", "--ff-only", "origin/main", "--quiet"); err != nil {
			return err
		}
		local, err := runOutput(e.repo, nil, "git", "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		remote, err := runOutput(e.repo, nil, "git", "rev-parse", "origin/main")
		if err != nil {
			return err
		}
		if strings.TrimSpace(local) != strings.TrimSpace(remote) {
			return errors.New("local main differs from origin/main")
		}
	}
	return nil
}

func githubRepoSlug(origin string) (string, error) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "", errors.New("repository origin is empty")
	}
	var path string
	switch {
	case strings.HasPrefix(origin, "git@github.com:"):
		path = strings.TrimPrefix(origin, "git@github.com:")
	default:
		u, err := url.Parse(origin)
		if err != nil {
			return "", fmt.Errorf("parse GitHub origin: %w", err)
		}
		if !strings.EqualFold(u.Hostname(), "github.com") {
			return "", fmt.Errorf("unsupported non-GitHub origin: %s", origin)
		}
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("cannot derive owner/repo from origin: %s", origin)
	}
	return parts[0] + "/" + parts[1], nil
}

func canonicalOrigin(origin string) bool { _, err := githubRepoSlug(origin); return err == nil }

func readInstruction(source string) (string, error) {
	if source == "-" {
		data, err := io.ReadAll(os.Stdin)
		return string(data), err
	}
	data, err := os.ReadFile(source)
	return string(data), err
}

func (e *engine) issueContext() (string, error) {
	out, err := runOutput(e.repo, nil, "gh", "issue", "view", strconvI(e.issue), "--repo", e.repoSlug, "--json", "title,body")
	if err != nil {
		return "", err
	}
	var v struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", err
	}
	return "ISSUE TITLE: " + v.Title + "\n\nISSUE BODY:\n" + v.Body, nil
}

func (e *engine) renderTemplate(name, issueContext, instruction string) (string, error) {
	path := filepath.Join(e.repo, ".codex", "prompts", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	text = strings.ReplaceAll(text, "{{ISSUE}}", strconvI(e.issue))
	text = strings.ReplaceAll(text, "{{CONTRACT}}", e.contract)
	text = strings.ReplaceAll(text, "{{ISSUE_CONTEXT}}", issueContext)
	text = strings.ReplaceAll(text, "{{LEAD_INSTRUCTION}}", instruction)
	return text, nil
}

func runOutput(dir string, env []string, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func runCommand(dir string, env []string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func strconvI(v int) string { return fmt.Sprintf("%d", v) }
