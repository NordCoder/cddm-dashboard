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
	Color     ColorMode
	Profile   string
	Repo      string
	Model     string
	Reasoning string
}

type profileConfig struct {
	Repo      string `json:"repo,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

type userConfig struct {
	Version  int                      `json:"version"`
	Profiles map[string]profileConfig `json:"profiles"`
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
		if command != "" && command == "profile" {
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
		case arg == "-p" || arg == "--profile":
			value, err := consumeValue(arg)
			if err != nil {
				return opts, "", nil, err
			}
			opts.Profile = strings.TrimSpace(value)
		case strings.HasPrefix(arg, "--profile="):
			opts.Profile = strings.TrimSpace(strings.TrimPrefix(arg, "--profile="))
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

	if opts.Profile != "" {
		if err := validateProfileName(opts.Profile); err != nil {
			return opts, "", nil, err
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
		return userConfig{Version: configVersion, Profiles: map[string]profileConfig{}}, nil
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
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]profileConfig{}
	}
	for name := range cfg.Profiles {
		if err := validateProfileName(name); err != nil {
			return userConfig{}, fmt.Errorf("invalid profile in config: %w", err)
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
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]profileConfig{}
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
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
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
	return os.Rename(tmpName, path)
}

func validateProfileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("profile name is empty")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("invalid profile name %q", name)
	}
	return nil
}

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

func resolveInvocation(opts globalOptions) (string, profileConfig, error) {
	var selected profileConfig
	if opts.Profile != "" {
		cfg, err := loadUserConfig()
		if err != nil {
			return "", selected, err
		}
		var ok bool
		selected, ok = cfg.Profiles[opts.Profile]
		if !ok {
			return "", selected, fmt.Errorf("profile %q does not exist", opts.Profile)
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
		if opts.Profile != "" && selected.Repo == "" && opts.Repo == "" {
			return "", selected, fmt.Errorf("profile %q has no repo and current directory is not a Git repository", opts.Profile)
		}
		return "", selected, fmt.Errorf("resolve target repository: %w", err)
	}
	return root, selected, nil
}

func effectiveExecutionOptions(opts globalOptions, profile profileConfig) executionOptions {
	model := profile.Model
	reasoning := profile.Reasoning
	if opts.Model != "" {
		model = opts.Model
	}
	if opts.Reasoning != "" {
		reasoning = opts.Reasoning
	}
	return executionOptions{Model: model, Reasoning: reasoning}
}

func commandProfile(ui *UI, opts globalOptions, args []string) int {
	if len(args) == 0 {
		ui.errorf("usage: cddm profile <set|list|show|remove> ...")
		return 2
	}
	cfg, err := loadUserConfig()
	if err != nil {
		ui.errorf("load profiles: %v", err)
		return 1
	}

	switch args[0] {
	case "list":
		if len(args) != 1 {
			ui.errorf("usage: cddm profile list")
			return 2
		}
		names := make([]string, 0, len(cfg.Profiles))
		for name := range cfg.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Println("No CDDM profiles configured.")
			return 0
		}
		for _, name := range names {
			p := cfg.Profiles[name]
			fmt.Printf("%-18s repo=%s", name, defaultString(p.Repo, "-"))
			if p.Model != "" {
				fmt.Printf("  model=%s", p.Model)
			}
			if p.Reasoning != "" {
				fmt.Printf("  reasoning=%s", p.Reasoning)
			}
			fmt.Println()
		}
		return 0
	case "show":
		if len(args) != 2 {
			ui.errorf("usage: cddm profile show <name>")
			return 2
		}
		p, ok := cfg.Profiles[args[1]]
		if !ok {
			ui.errorf("profile %q does not exist", args[1])
			return 1
		}
		data, _ := json.MarshalIndent(p, "", "  ")
		fmt.Println(string(data))
		return 0
	case "remove":
		if len(args) != 2 {
			ui.errorf("usage: cddm profile remove <name>")
			return 2
		}
		if _, ok := cfg.Profiles[args[1]]; !ok {
			ui.errorf("profile %q does not exist", args[1])
			return 1
		}
		delete(cfg.Profiles, args[1])
		if err := saveUserConfig(cfg); err != nil {
			ui.errorf("save profiles: %v", err)
			return 1
		}
		fmt.Printf("Removed profile %s.\n", args[1])
		return 0
	case "set":
		return commandProfileSet(ui, opts, cfg, args[1:])
	default:
		ui.errorf("unknown profile command %q", args[0])
		return 2
	}
}

func commandProfileSet(ui *UI, global globalOptions, cfg userConfig, args []string) int {
	if len(args) == 0 {
		ui.errorf("usage: cddm profile set <name> [--repo <path>] [--model <model>] [--reasoning <effort>]")
		return 2
	}
	name := args[0]
	if err := validateProfileName(name); err != nil {
		ui.errorf("%v", err)
		return 2
	}
	p := cfg.Profiles[name]
	repoArg := strings.TrimSpace(global.Repo)
	model := strings.TrimSpace(global.Model)
	reasoning := strings.TrimSpace(global.Reasoning)

	for i := 1; i < len(args); i++ {
		arg := args[i]
		value := func(flag string) (string, bool) {
			if i+1 >= len(args) {
				ui.errorf("%s requires a value", flag)
				return "", false
			}
			i++
			return strings.TrimSpace(args[i]), true
		}
		switch {
		case arg == "--repo" || arg == "-C":
			v, ok := value(arg)
			if !ok {
				return 2
			}
			repoArg = v
		case strings.HasPrefix(arg, "--repo="):
			repoArg = strings.TrimSpace(strings.TrimPrefix(arg, "--repo="))
		case arg == "--model":
			v, ok := value(arg)
			if !ok {
				return 2
			}
			model = v
		case strings.HasPrefix(arg, "--model="):
			model = strings.TrimSpace(strings.TrimPrefix(arg, "--model="))
		case arg == "--reasoning":
			v, ok := value(arg)
			if !ok {
				return 2
			}
			reasoning = v
		case strings.HasPrefix(arg, "--reasoning="):
			reasoning = strings.TrimSpace(strings.TrimPrefix(arg, "--reasoning="))
		default:
			ui.errorf("unknown profile option %q", arg)
			return 2
		}
	}

	if repoArg != "" {
		root, err := resolveGitRoot(repoArg)
		if err != nil {
			ui.errorf("profile repo: %v", err)
			return 1
		}
		p.Repo = root
	} else if p.Repo == "" {
		root, err := resolveGitRoot("")
		if err != nil {
			ui.errorf("new profile requires --repo when current directory is not a Git repository")
			return 1
		}
		p.Repo = root
	}
	if model != "" {
		p.Model = model
	}
	if reasoning != "" {
		p.Reasoning = reasoning
	}
	cfg.Profiles[name] = p
	if err := saveUserConfig(cfg); err != nil {
		ui.errorf("save profile: %v", err)
		return 1
	}
	fmt.Printf("Saved profile %s.\n", name)
	fmt.Printf("  repo:      %s\n", p.Repo)
	fmt.Printf("  model:     %s\n", defaultString(p.Model, "runtime default"))
	fmt.Printf("  reasoning: %s\n", defaultString(p.Reasoning, "runtime default"))
	return 0
}
