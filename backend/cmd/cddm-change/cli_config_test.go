package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCLIGlobalOptionsAroundCommand(t *testing.T) {
	opts, command, args, err := parseCLI([]string{
		"-p", "dashboard", "start", "17", "--model", "gpt-5.6-luna", "--reasoning=high", "--color=never",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command != "start" || len(args) != 1 || args[0] != "17" {
		t.Fatalf("command=%q args=%q", command, args)
	}
	if opts.Profile != "dashboard" || opts.Model != "gpt-5.6-luna" || opts.Reasoning != "high" || opts.Color != ColorNever {
		t.Fatalf("opts=%#v", opts)
	}
}

func TestParseCLIProfileCommandPreservesProfileFlags(t *testing.T) {
	opts, command, args, err := parseCLI([]string{"profile", "set", "work", "--model", "gpt-test", "--reasoning", "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if command != "profile" || opts.Model != "" || len(args) != 6 {
		t.Fatalf("command=%q opts=%#v args=%q", command, opts, args)
	}
}

func TestUserConfigRoundTripAndMalformedFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cddm", "config.json")
	t.Setenv("CDDM_CONFIG", path)
	cfg := userConfig{Version: configVersion, Profiles: map[string]profileConfig{
		"dashboard": {Repo: "/tmp/repo", Model: "gpt-test", Reasoning: "high"},
	}}
	if err := saveUserConfig(cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config permissions=%o, want private", info.Mode().Perm())
	}
	got, err := loadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles["dashboard"].Model != "gpt-test" || got.Profiles["dashboard"].Reasoning != "high" {
		t.Fatalf("config=%#v", got)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadUserConfig(); err == nil {
		t.Fatal("malformed config was accepted")
	}
}

func TestResolveInvocationProfileAndExplicitRepoPrecedence(t *testing.T) {
	profileRepo := initGitRepo(t)
	explicitRepo := initGitRepo(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("CDDM_CONFIG", path)
	cfg := userConfig{Version: configVersion, Profiles: map[string]profileConfig{
		"work": {Repo: profileRepo, Model: "profile-model", Reasoning: "medium"},
	}}
	if err := saveUserConfig(cfg); err != nil {
		t.Fatal(err)
	}

	root, p, err := resolveInvocation(globalOptions{Profile: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if root != profileRepo || p.Model != "profile-model" {
		t.Fatalf("root=%q profile=%#v", root, p)
	}

	root, p, err = resolveInvocation(globalOptions{Profile: "work", Repo: explicitRepo})
	if err != nil {
		t.Fatal(err)
	}
	if root != explicitRepo || p.Model != "profile-model" {
		t.Fatalf("explicit root=%q profile=%#v", root, p)
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

func TestUnknownProfileFailsClosed(t *testing.T) {
	t.Setenv("CDDM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if _, _, err := resolveInvocation(globalOptions{Profile: "missing"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err=%v", err)
	}
}

func TestGitHubRepoSlugSupportsCommonOrigins(t *testing.T) {
	cases := map[string]string{
		"git@github.com:NordCoder/cddm-dashboard.git":          "NordCoder/cddm-dashboard",
		"https://github.com/NordCoder/misak-website.git":       "NordCoder/misak-website",
		"ssh://git@github.com/NordCoder/unmatched-web.git":      "NordCoder/unmatched-web",
		"https://github.com/NordCoder/haze-sync":                "NordCoder/haze-sync",
	}
	for origin, want := range cases {
		got, err := githubRepoSlug(origin)
		if err != nil || got != want {
			t.Fatalf("origin=%q got=%q err=%v want=%q", origin, got, err, want)
		}
	}
	if _, err := githubRepoSlug("git@gitlab.com:owner/repo.git"); err == nil {
		t.Fatal("non-GitHub origin was accepted")
	}
}

func TestExecutionOptionPrecedence(t *testing.T) {
	profile := profileConfig{Model: "profile-model", Reasoning: "low"}
	opts := globalOptions{Model: "cli-model", Reasoning: "high"}
	got := resolveExecutionOptions(opts, profile)
	if got.ProfileModel != "profile-model" || got.ProfileReasoning != "low" || got.Model != "cli-model" || got.Reasoning != "high" {
		t.Fatalf("execution options=%#v", got)
	}
}
