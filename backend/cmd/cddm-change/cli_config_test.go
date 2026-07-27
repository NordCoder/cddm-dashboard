package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCLICodexProfileAndWorkspace(t *testing.T) {
	opts, command, args, err := parseCLI([]string{
		"-w", "dashboard", "-p", "deep-review", "start", "17", "--model", "gpt-5.6-luna", "--reasoning=high", "--color=never",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command != "start" || len(args) != 1 || args[0] != "17" {
		t.Fatalf("command=%q args=%q", command, args)
	}
	if opts.Workspace != "dashboard" || opts.CodexProfile != "deep-review" || opts.Model != "gpt-5.6-luna" || opts.Reasoning != "high" || opts.Color != ColorNever {
		t.Fatalf("opts=%#v", opts)
	}
}

func TestParseCLIWorkspaceCommandPreservesWorkspaceFlags(t *testing.T) {
	opts, command, args, err := parseCLI([]string{"workspace", "set", "work", "--model", "gpt-test", "--reasoning", "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if command != "workspace" || opts.Model != "" || len(args) != 6 {
		t.Fatalf("command=%q opts=%#v args=%q", command, opts, args)
	}
}

func TestUserConfigRoundTripMigratesLegacyProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cddm", "config.json")
	t.Setenv("CDDM_CONFIG", path)
	legacy := `{"version":1,"profiles":{"dashboard":{"repo":"/tmp/repo","model":"gpt-test","reasoning":"high"}}}`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspaces["dashboard"].Model != "gpt-test" {
		t.Fatalf("config=%#v", cfg)
	}
	if err := saveUserConfig(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"profiles"`) || !strings.Contains(string(data), `"workspaces"`) {
		t.Fatalf("saved config=%s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config permissions=%o, want private", info.Mode().Perm())
	}
}

func TestMalformedConfigFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("CDDM_CONFIG", path)
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadUserConfig(); err == nil {
		t.Fatal("malformed config was accepted")
	}
}

func TestResolveInvocationWorkspaceAndExplicitRepoPrecedence(t *testing.T) {
	workspaceRepo := initGitRepo(t)
	explicitRepo := initGitRepo(t)
	t.Setenv("CDDM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	cfg := userConfig{Version: configVersion, Workspaces: map[string]workspaceConfig{"work": {Repo: workspaceRepo, Model: "workspace-model", Reasoning: "medium"}}}
	if err := saveUserConfig(cfg); err != nil {
		t.Fatal(err)
	}
	root, w, err := resolveInvocation(globalOptions{Workspace: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if root != workspaceRepo || w.Model != "workspace-model" {
		t.Fatalf("root=%q workspace=%#v", root, w)
	}
	root, w, err = resolveInvocation(globalOptions{Workspace: "work", Repo: explicitRepo})
	if err != nil {
		t.Fatal(err)
	}
	if root != explicitRepo || w.Model != "workspace-model" {
		t.Fatalf("root=%q workspace=%#v", root, w)
	}
}

func TestResolveInvocationFromNestedWorkingDirectory(t *testing.T) {
	repo := initGitRepo(t)
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CDDM_REPO_ROOT", "")
	root, _, err := resolveInvocation(globalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if root != repo {
		t.Fatalf("root=%q want=%q", root, repo)
	}
}

func TestUnknownWorkspaceFailsClosed(t *testing.T) {
	t.Setenv("CDDM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if _, _, err := resolveInvocation(globalOptions{Workspace: "missing"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err=%v", err)
	}
}

func TestGitHubRepoSlugSupportsCommonOrigins(t *testing.T) {
	cases := map[string]string{
		"git@github.com:NordCoder/cddm-dashboard.git":      "NordCoder/cddm-dashboard",
		"https://github.com/NordCoder/misak-website.git":   "NordCoder/misak-website",
		"ssh://git@github.com/NordCoder/unmatched-web.git": "NordCoder/unmatched-web",
	}
	for origin, want := range cases {
		got, err := githubRepoSlug(origin)
		if err != nil || got != want {
			t.Fatalf("origin=%q got=%q err=%v", origin, got, err)
		}
	}
}

func TestExecutionOptionPrecedence(t *testing.T) {
	workspace := workspaceConfig{Model: "workspace-model", Reasoning: "low"}
	opts := globalOptions{Model: "cli-model", Reasoning: "high", CodexProfile: "deep-review"}
	got := resolveExecutionOptions(opts, workspace)
	if got.ProfileModel != "workspace-model" || got.ProfileReasoning != "low" || got.Model != "cli-model" || got.Reasoning != "high" || got.CodexProfile != "deep-review" {
		t.Fatalf("execution options=%#v", got)
	}
}
