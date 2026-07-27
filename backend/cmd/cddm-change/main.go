package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var buildRevision = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 1 && args[0] == "__build-revision" {
		fmt.Println(buildRevision)
		return 0
	}

	mode, args, err := parseColorMode(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ui := newUI(mode)

	if len(args) == 0 {
		printUsage(os.Stderr)
		return 2
	}

	if strings.HasPrefix(args[0], "__") {
		return runInternal(ui, args)
	}

	repo, err := repoRoot()
	if err != nil {
		ui.errorf("cannot resolve repository: %v", err)
		return 1
	}

	switch args[0] {
	case "status":
		return commandStatus(ui, repo, args[1:])
	case "watch":
		if len(args) >= 2 {
			if issue, parseErr := parseIssue(args[1]); parseErr == nil {
				statePath, historyPath, _ := statePaths(repo, issue)
				if state, loadErr := loadState(statePath); loadErr == nil && state.ActiveMode == "" {
					printStatusDashboard(ui, os.Stdout, issue, state, historyPath)
					fmt.Fprintf(os.Stdout, "\n%s\n", ui.style(os.Stdout, ansiDim, "observer: no active turn"))
					return 0
				}
			}
		}
		return commandWatch(ui, repo, args[1:])
	case "logs":
		return commandLogs(ui, repo, args[1:])
	case "turns":
		return commandTurns(ui, repo, args[1:])
	case "start", "resume", "rotate", "recover", "stop", "reconcile":
		return commandMutating(ui, repo, args[0], args[1:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return 0
	default:
		ui.errorf("unknown command %q", args[0])
		printUsage(os.Stderr)
		return 2
	}
}

func runInternal(ui *UI, args []string) int {
	switch args[0] {
	case "__proxy":
		return commandProxy(ui, args[1:])
	case "__v2tee":
		return commandV2Tee(ui, args[1:])
	case "__record-recovery":
		return commandRecordRecovery(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown internal command %q\n", args[0])
		return 2
	}
}

func parseColorMode(args []string) (ColorMode, []string, error) {
	mode := ColorAuto
	if env := strings.TrimSpace(os.Getenv("CDDM_COLOR")); env != "" {
		parsed, err := parseColor(env)
		if err != nil {
			return mode, args, err
		}
		mode = parsed
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "--color=") {
		parsed, err := parseColor(strings.TrimPrefix(args[0], "--color="))
		if err != nil {
			return mode, args, err
		}
		mode = parsed
		args = args[1:]
	}
	return mode, args, nil
}

func repoRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("CDDM_REPO_ROOT")); root != "" {
		return filepath.Abs(root)
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return filepath.Abs(strings.TrimSpace(string(out)))
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, `CDDM Host Runtime

Usage:
  scripts/cddm-codex-change.sh start     <issue> [model] [reasoning]
  scripts/cddm-codex-change.sh resume    <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh rotate    <issue> <instruction-file|-> [model] [reasoning]
  scripts/cddm-codex-change.sh recover   <issue>
  scripts/cddm-codex-change.sh stop      <issue>
  scripts/cddm-codex-change.sh reconcile <issue>
  scripts/cddm-codex-change.sh status    <issue> [--json]
  scripts/cddm-codex-change.sh watch     <issue> [--stall-seconds N]
  scripts/cddm-codex-change.sh logs      <issue> [--raw|--v2]
  scripts/cddm-codex-change.sh turns     <issue> [--limit N]

Color:
  --color=auto|always|never
  CDDM_COLOR=auto|always|never
  NO_COLOR=1 disables color`)
}
