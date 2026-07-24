package config

import (
	"strings"
	"testing"
	"time"
)

func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_ADDR", "APP_DATABASE_PATH", "APP_SHUTDOWN_TIMEOUT", "GITHUB_TOKEN",
		"GITHUB_API_BASE_URL", "GITHUB_REQUEST_TIMEOUT", "GITHUB_SYNC_TIMEOUT",
		"GITHUB_DEFAULT_POLL_INTERVAL", "GITHUB_POLL_SCAN_INTERVAL", "GITHUB_MAX_PAGES",
		"GITHUB_MAX_ITEMS", "GITHUB_MAX_SYNC_CONCURRENCY", "OPENCODE_ENABLED", "OPENCODE_ENDPOINT",
		"OPENCODE_PROVIDER", "OPENCODE_MODEL", "OPENCODE_AGENT", "OPENCODE_USERNAME",
		"OPENCODE_PASSWORD", "OPENCODE_TIMEOUT", "OPENCODE_MAX_REQUEST_BYTES",
		"PROMPT_FALLBACK_ENABLED", "PROMPT_EVIDENCE_LIMIT", "PROMPT_EVIDENCE_CHARS",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Address != defaultAddress || cfg.DatabasePath != defaultDatabasePath {
		t.Fatalf("unexpected application defaults: %#v", cfg)
	}
	if cfg.GitHubAPIBaseURL != defaultGitHubAPIBaseURL || cfg.GitHubDefaultPollInterval != defaultGitHubPollInterval {
		t.Fatalf("unexpected GitHub defaults: %#v", cfg)
	}
	if cfg.GitHubToken != "" || cfg.OpenCodePassword != "" {
		t.Fatalf("credentials must default empty: %#v", cfg)
	}
	if cfg.OpenCodeEnabled || !cfg.PromptFallbackEnabled || cfg.OpenCodeAgent != "prompt-planner" {
		t.Fatalf("unexpected planning defaults: %#v", cfg)
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("APP_ADDR", "127.0.0.1:9000")
	t.Setenv("APP_DATABASE_PATH", "/tmp/cddm.db")
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("GITHUB_TOKEN", "top-secret")
	t.Setenv("GITHUB_API_BASE_URL", "https://github.example/api/v3/")
	t.Setenv("GITHUB_REQUEST_TIMEOUT", "4s")
	t.Setenv("GITHUB_SYNC_TIMEOUT", "45s")
	t.Setenv("GITHUB_DEFAULT_POLL_INTERVAL", "2m")
	t.Setenv("GITHUB_POLL_SCAN_INTERVAL", "7s")
	t.Setenv("GITHUB_MAX_PAGES", "3")
	t.Setenv("GITHUB_MAX_ITEMS", "125")
	t.Setenv("GITHUB_MAX_SYNC_CONCURRENCY", "2")
	t.Setenv("OPENCODE_ENABLED", "true")
	t.Setenv("OPENCODE_ENDPOINT", "http://opencode:4096")
	t.Setenv("OPENCODE_PROVIDER", "provider")
	t.Setenv("OPENCODE_MODEL", "model")
	t.Setenv("OPENCODE_AGENT", "restricted-planner")
	t.Setenv("OPENCODE_PASSWORD", "planner-secret")
	t.Setenv("OPENCODE_TIMEOUT", "8s")
	t.Setenv("OPENCODE_MAX_REQUEST_BYTES", "65536")
	t.Setenv("PROMPT_FALLBACK_ENABLED", "false")
	t.Setenv("PROMPT_EVIDENCE_LIMIT", "16")
	t.Setenv("PROMPT_EVIDENCE_CHARS", "2048")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Address != "127.0.0.1:9000" || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("unexpected application config: %#v", cfg)
	}
	if cfg.GitHubToken != "top-secret" || cfg.GitHubRequestTimeout != 4*time.Second || cfg.GitHubMaxPages != 3 {
		t.Fatalf("unexpected GitHub config: %#v", cfg)
	}
	if !cfg.OpenCodeEnabled || cfg.OpenCodeModel != "model" || cfg.OpenCodeTimeout != 8*time.Second || cfg.PromptFallbackEnabled {
		t.Fatalf("unexpected planning config: %#v", cfg)
	}
}

func TestLoadRejectsInvalidValuesWithoutEchoingSecret(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("GITHUB_TOKEN", "top-secret")
	t.Setenv("OPENCODE_PASSWORD", "planner-secret")
	t.Setenv("OPENCODE_ENABLED", "true")
	t.Setenv("OPENCODE_PROVIDER", "provider")
	t.Setenv("OPENCODE_MODEL", "model")
	t.Setenv("OPENCODE_MAX_REQUEST_BYTES", "many")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), "planner-secret") {
		t.Fatalf("error leaked credential: %v", err)
	}
}

func TestLoadRequiresOpenCodeModelWhenEnabled(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("OPENCODE_ENABLED", "true")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "OPENCODE_PROVIDER") {
		t.Fatalf("Load() error = %v, want missing model/provider error", err)
	}
}
