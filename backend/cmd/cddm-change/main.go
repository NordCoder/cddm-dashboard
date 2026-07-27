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
	if len(args) > 0 && strings.HasPrefix(args[0], "__") {
		mode := ColorAuto
		if env := strings.TrimSpace(os.Getenv("CDDM_COLOR")); env != "" {
			parsed, err := parseColor(env)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			mode = parsed
		}
		return runInternal(newUI(mode), args)
	}

	opts, command, commandArgs, err := parseCLI(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ui := newUI(opts.Color)

	if command == "" {
		printUsage(os.Stderr)
		return 2
	}
	if command == "help" || command == "-h" || command == "--help" {
		printUsage(os.Stdout)
		return 0
	}
	if command == "version" {
		if len(commandArgs) != 0 {
			ui.errorf("usage: cddm version")
			return 2
		}
		fmt.Printf("cddm %s\n", buildRevision)
		return 0
	}
	if command == "profile" {
		return commandProfile(ui, opts, commandArgs)
	}

	repo, profile, err := resolveInvocation(opts)
	if err != nil {
		ui.errorf("%v", err)
		return 1
	}
	execOpts := effectiveExecutionOptions(opts, profile)

	switch command {
	case "status":
		return commandStatus(ui, repo, commandArgs)
	case "watch":
		if len(commandArgs) >= 1 {
			if issue, parseErr := parseIssue(commandArgs[0]); parseErr == nil {
				statePath, historyPath, _ := statePaths(repo, issue)
				if state, loadErr := loadState(statePath); loadErr == nil && state.ActiveMode == "" {
					printStatusDashboard(ui, os.Stdout, issue, state, historyPath)
					fmt.Fprintf(os.Stdout, "\n%s\n", ui.style(os.Stdout, ansiDim, "observer: no active turn"))
					return 0
				}
			}
		}
		return commandWatch(ui, repo, commandArgs)
	case "logs":
		return commandLogs(ui, repo, commandArgs)
	case "turns":
		return commandTurns(ui, repo, commandArgs)
	case "start", "resume", "rotate", "recover", "stop", "reconcile":
		return commandMutatingWithOptions(ui, repo, command, commandArgs, execOpts)
	default:
		ui.errorf("unknown command %q", command)
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

// parseColorMode remains for compatibility with focused tests and internal callers.
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

// repoRoot preserves the old environment-aware repository resolver for compatibility.
func repoRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("CDDM_REPO_ROOT")); root != "" {
		return resolveGitRoot(root)
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
  cddm [global options] <command> [args]

Global options:
  -C, --repo <path>          target repository (otherwise profile or current Git repo)
  -p, --profile <name>       use named repository/execution profile
      --model <model>        override Codex model for start/resume/rotate
      --reasoning <effort>   override reasoning effort for start/resume/rotate
      --color auto|always|never

Runtime:
  cddm start     <issue> [legacy-model] [legacy-reasoning]
  cddm resume    <issue> <instruction-file|-> [legacy-model] [legacy-reasoning]
  cddm rotate    <issue> <instruction-file|-> [legacy-model] [legacy-reasoning]
  cddm recover   <issue>
  cddm stop      <issue>
  cddm reconcile <issue>
  cddm status    <issue> [--json]
  cddm watch     <issue> [--stall-seconds N]
  cddm logs      <issue> [--raw|--v2]
  cddm turns     <issue> [--limit N]

Profiles:
  cddm profile set <name> [--repo <path>] [--model <model>] [--reasoning <effort>]
  cddm profile list
  cddm profile show <name>
  cddm profile remove <name>

Examples:
  cddm status 17
  cddm -p dashboard status 17
  cddm -p dashboard --model gpt-5.6-luna --reasoning medium resume 17 /tmp/fix.txt
  cddm -C ~/projects/other-repo start 42 --model gpt-5.6-terra --reasoning medium

Environment:
  CDDM_CONFIG       override profile config path
  CDDM_REPO_ROOT    compatibility repository default
  CDDM_COLOR        auto|always|never
  NO_COLOR=1        disable color`)
}
