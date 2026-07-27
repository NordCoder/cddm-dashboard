package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const configVersion = 1

type globalOptions struct {
	Color        ColorMode
	Workspace    string
	CodexProfile string
	Repo         string
	Model        string
	Reasoning    string
}

type workspaceConfig struct {
	Repo      string `json:"repo,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

type userConfig struct {
	Version          int                        `json:"version"`
	Workspaces       map[string]workspaceConfig `json:"workspaces"`
	LegacyProfiles   map[string]workspaceConfig `json:"profiles,omitempty"`
}

func parseCLI(args []string) (globalOptions, string, []string, error) {
	opts := globalOptions{Color: ColorAuto}
	if env := strings.TrimSpace(os.Getenv("CDDM_COLOR")); env != "" {
		mode, err := parseColor(env)
		if err != nil {
			return opts, "", nil, err
		}
		opts.Color = mode
	}

	var command string
	var commandArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if command == "workspace" {
			commandArgs = append(commandArgs, args[i:]...)
			break
		}
		consumeValue := func(name string) (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		handled := true
		switch {
		case arg == "-w" || arg == "--workspace":
			value, err := consumeValue(arg)
			if err != nil {
				return opts, "", nil, err
			}
			opts.Workspace = strings.TrimSpace(value)
		case strings.HasPrefix(arg, "--workspace="):
			opts.Workspace = strings.TrimSpace(strings.TrimPrefix(arg, "--workspace="))
		case arg == "-p" || arg == "--profile":
			value, err := consumeValue(arg)
			if err != nil {
				return opts, "", nil, err
			}
			opts.CodexProfile = strings.TrimSpace(value)
		case strings.HasPrefix(arg, "--profile="):
			opts.CodexProfile = strings.TrimSpace(strings.TrimPrefix(arg, "--profile="))
		case arg == "-C" || arg == "--repo":
			value, err := consumeValue(arg)
			if err != nil {
				return opts, "", nil, err
			}
			opts.Repo = strings.TrimSpace(value)
		case strings.HasPrefix(arg, "--repo="):
			opts.Repo = strings.TrimSpace(strings.TrimPrefix(arg, "--repo="))
		case arg == "--model":
			value, err := consumeValue(arg)
			if err != nil {
				return opts, "", nil, err
			}
			opts.Model = strings.TrimSpace(value)
		case strings.HasPrefix(arg, "--model="):
			opts.Model = strings.TrimSpace(strings.TrimPrefix(arg, "--model="))
		case arg == "--reasoning":
			value, err := consumeValue(arg)
			if err != nil {
				return opts, "", nil, err
			}
			opts.Reasoning = strings.TrimSpace(value)
		case strings.HasPrefix(arg, "--reasoning="):
			opts.Reasoning = strings.TrimSpace(strings.TrimPrefix(arg, "--reasoning="))
		case arg == "--color":
			value, err := consumeValue(arg)
			if err != nil {
				return opts, "", nil, err
			}
			mode, err := parseColor(value)
			if err != nil {
				return opts, "", nil, err
			}
			opts.Color = mode
		case strings.HasPrefix(arg, "--color="):
			mode, err := parseColor(strings.TrimPrefix(arg, "--color="))
			if err != nil {
				return opts, "", nil, err
			}
			opts.Color = mode
		default:
			handled = false
		}
		if handled {
			continue
		}
		if command == "" {
			if strings.HasPrefix(arg, "-") {
				return opts, "", nil, fmt.Errorf("unknown global option %q", arg)
			}
			command = arg
			continue
		}
		commandArgs = append(commandArgs, arg)
	}
	for label, value := range map[string]string{"workspace": opts.Workspace, "profile": opts.CodexProfile} {
		if value != "" {
			if err := validateName(value, label); err != nil {
				return opts, "", nil, err
			}
		}
	}
	return opts, command, commandArgs, nil
}

func configPath() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("CDDM_CONFIG")); explicit != "" {
		return filepath.Abs(explicit)
	}
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "cddm", "config.json"), nil
}

func loadUserConfig() (userConfig, error) {
	path, err := configPath()
	if err != nil {
		return userConfig{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return userConfig{Version: configVersion, Workspaces: map[string]workspaceConfig{}}, nil
	}
	if err != nil {
		return userConfig{}, err
	}
	var cfg userConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return userConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Version != configVersion {
		return userConfig{}, fmt.Errorf("unsupported CDDM config version %d", cfg.Version)
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]workspaceConfig{}
	}
	for name, legacy := range cfg.LegacyProfiles {
		if _, exists := cfg.Workspaces[name]; !exists {
			cfg.Workspaces[name] = legacy
		}
	}
	cfg.LegacyProfiles = nil
	for name := range cfg.Workspaces {
		if err := validateName(name, "workspace"); err != nil {
			return userConfig{}, fmt.Errorf("invalid workspace in config: %w", err)
		}
	}
	return cfg, nil
}

func saveUserConfig(cfg userConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	cfg.Version = configVersion
	cfg.LegacyProfiles = nil
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]workspaceConfig{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func validateName(name, kind string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s name is empty", kind)
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("invalid %s name %q", kind, name)
	}
	return nil
}

func validateProfileName(name string) error { return validateName(name, "profile") }

func resolveGitRoot(path string) (string, error) {
	var cmd *exec.Cmd
	if strings.TrimSpace(path) == "" {
		cmd = exec.Command("git", "rev-parse", "--show-toplevel")
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		cmd = exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel")
	}
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("not inside a Git repository")
	}
	return filepath.Abs(strings.TrimSpace(string(out)))
}

func resolveInvocation(opts globalOptions) (string, workspaceConfig, error) {
	var selected workspaceConfig
	if opts.Workspace != "" {
		cfg, err := loadUserConfig()
		if err != nil {
			return "", selected, err
		}
		var ok bool
		selected, ok = cfg.Workspaces[opts.Workspace]
		if !ok {
			return "", selected, fmt.Errorf("workspace %q does not exist", opts.Workspace)
		}
	}
	candidate := strings.TrimSpace(opts.Repo)
	if candidate == "" && selected.Repo != "" {
		candidate = selected.Repo
	}
	if candidate == "" {
		candidate = strings.TrimSpace(os.Getenv("CDDM_REPO_ROOT"))
	}
	root, err := resolveGitRoot(candidate)
	if err != nil {
		if opts.Workspace != "" && selected.Repo == "" && opts.Repo == "" {
			return "", selected, fmt.Errorf("workspace %q has no repo and current directory is not a Git repository", opts.Workspace)
		}
		return "", selected, fmt.Errorf("resolve target repository: %w", err)
	}
	return root, selected, nil
}

func commandWorkspace(ui *UI, opts globalOptions, args []string) int {
	if len(args) == 0 {
		ui.errorf("usage: cddm workspace <set|list|show|remove> ...")
		return 2
	}
	cfg, err := loadUserConfig()
	if err != nil {
		ui.errorf("load workspaces: %v", err)
		return 1
	}
	switch args[0] {
	case "list":
		names := make([]string, 0, len(cfg.Workspaces))
		for name := range cfg.Workspaces {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Println("No CDDM workspaces configured.")
			return 0
		}
		for _, name := range names {
			w := cfg.Workspaces[name]
			fmt.Printf("%-18s repo=%s", name, defaultString(w.Repo, "-"))
			if w.Model != "" { fmt.Printf("  model=%s", w.Model) }
			if w.Reasoning != "" { fmt.Printf("  reasoning=%s", w.Reasoning) }
			fmt.Println()
		}
		return 0
	case "show":
		if len(args) != 2 { ui.errorf("usage: cddm workspace show <name>"); return 2 }
		w, ok := cfg.Workspaces[args[1]]
		if !ok { ui.errorf("workspace %q does not exist", args[1]); return 1 }
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Println(string(data)); return 0
	case "remove":
		if len(args) != 2 { ui.errorf("usage: cddm workspace remove <name>"); return 2 }
		if _, ok := cfg.Workspaces[args[1]]; !ok { ui.errorf("workspace %q does not exist", args[1]); return 1 }
		delete(cfg.Workspaces, args[1])
		if err := saveUserConfig(cfg); err != nil { ui.errorf("save workspaces: %v", err); return 1 }
		fmt.Printf("Removed workspace %s.\n", args[1]); return 0
	case "set":
		return commandWorkspaceSet(ui, opts, cfg, args[1:])
	default:
		ui.errorf("unknown workspace command %q", args[0]); return 2
	}
}

func commandWorkspaceSet(ui *UI, global globalOptions, cfg userConfig, args []string) int {
	if len(args) == 0 {
		ui.errorf("usage: cddm workspace set <name> [--repo <path>] [--model <model>] [--reasoning <effort>]")
		return 2
	}
	name := args[0]
	if err := validateName(name, "workspace"); err != nil { ui.errorf("%v", err); return 2 }
	w := cfg.Workspaces[name]
	repoArg, model, reasoning := strings.TrimSpace(global.Repo), strings.TrimSpace(global.Model), strings.TrimSpace(global.Reasoning)
	for i := 1; i < len(args); i++ {
		arg := args[i]
		value := func(flag string) (string, bool) {
			if i+1 >= len(args) { ui.errorf("%s requires a value", flag); return "", false }
			i++; return strings.TrimSpace(args[i]), true
		}
		switch {
		case arg == "--repo" || arg == "-C": v, ok := value(arg); if !ok { return 2 }; repoArg = v
		case strings.HasPrefix(arg, "--repo="): repoArg = strings.TrimSpace(strings.TrimPrefix(arg, "--repo="))
		case arg == "--model": v, ok := value(arg); if !ok { return 2 }; model = v
		case strings.HasPrefix(arg, "--model="): model = strings.TrimSpace(strings.TrimPrefix(arg, "--model="))
		case arg == "--reasoning": v, ok := value(arg); if !ok { return 2 }; reasoning = v
		case strings.HasPrefix(arg, "--reasoning="): reasoning = strings.TrimSpace(strings.TrimPrefix(arg, "--reasoning="))
		default: ui.errorf("unknown workspace option %q", arg); return 2
		}
	}
	if repoArg != "" {
		root, err := resolveGitRoot(repoArg); if err != nil { ui.errorf("workspace repo: %v", err); return 1 }; w.Repo = root
	} else if w.Repo == "" {
		root, err := resolveGitRoot(""); if err != nil { ui.errorf("new workspace requires --repo when current directory is not a Git repository"); return 1 }; w.Repo = root
	}
	if model != "" { w.Model = model }
	if reasoning != "" { w.Reasoning = reasoning }
	cfg.Workspaces[name] = w
	if err := saveUserConfig(cfg); err != nil { ui.errorf("save workspace: %v", err); return 1 }
	fmt.Printf("Saved workspace %s.\n", name)
	fmt.Printf("  repo:      %s\n", w.Repo)
	fmt.Printf("  model:     %s\n", defaultString(w.Model, "runtime default"))
	fmt.Printf("  reasoning: %s\n", defaultString(w.Reasoning, "runtime default"))
	return 0
}
