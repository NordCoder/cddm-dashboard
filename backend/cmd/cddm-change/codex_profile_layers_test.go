package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeCodexBaseConfigKeepsProviderAndExactWorktreeTrust(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "issue-19")
	source := []byte(`model = "gpt-5.6-luna"
model_provider = "openai"
approvals_reviewer = "user"
openai_base_url = "https://proxy.example/v1"

[model_providers.codexsale]
name = "Codex Sale"
base_url = "https://codex.sale/v1"
env_key = "CODEXSALE_API_KEY"
wire_api = "responses"
requires_openai_auth = false

[model_providers.codexsale.auth]
command = "/usr/local/bin/provider-auth"
args = ["--token"]

[mcp_servers.playwright]
command = "npx"
args = ["@playwright/mcp@latest"]

[projects."/home/user/other"]
trust_level = "trusted"

[tui]
theme = "catppuccin-mocha"
`)

	got, err := sanitizeCodexBaseConfig(source, worktree)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		`model_provider = "openai"`,
		`openai_base_url = "https://proxy.example/v1"`,
		`[model_providers.codexsale]`,
		`env_key = "CODEXSALE_API_KEY"`,
		`[model_providers.codexsale.auth]`,
		`command = "/usr/local/bin/provider-auth"`,
		`[projects."` + worktree + `"]`,
		`trust_level = "trusted"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("sanitized config missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{
		`model = "gpt-5.6-luna"`,
		`approvals_reviewer = "user"`,
		`[mcp_servers.playwright]`,
		`[projects."/home/user/other"]`,
		`[tui]`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sanitized config imported forbidden host setting %q:\n%s", forbidden, text)
		}
	}
}

func TestInstallWorkerBaseConfigSupportsInheritedCustomProvider(t *testing.T) {
	root := t.TempDir()
	host := filepath.Join(root, "host")
	worker := filepath.Join(root, "worker")
	worktree := filepath.Join(root, "repo", ".worktrees", "issue-19")
	if err := os.MkdirAll(host, 0o700); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(host, "config.toml")
	if err := os.WriteFile(base, []byte(`[model_providers.codexsale]
name = "Codex Sale"
base_url = "https://codex.sale/v1"
env_key = "CODEXSALE_API_KEY"
wire_api = "responses"
requires_openai_auth = false

[mcp_servers.playwright]
command = "npx"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installWorkerBaseConfig(base, worker, worktree); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(worker, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[model_providers.codexsale]") || !strings.Contains(text, `env_key = "CODEXSALE_API_KEY"`) {
		t.Fatalf("custom provider missing from worker base:\n%s", text)
	}
	if strings.Contains(text, "mcp_servers") {
		t.Fatalf("host MCP leaked into worker base:\n%s", text)
	}
	if !strings.Contains(text, worktree) {
		t.Fatalf("exact worktree trust missing:\n%s", text)
	}
	info, err := os.Stat(filepath.Join(worker, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("worker base mode=%o want=600", info.Mode().Perm())
	}
}
