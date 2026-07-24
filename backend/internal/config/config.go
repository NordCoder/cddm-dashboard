package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress                  = ":8080"
	defaultDatabasePath             = "data/cddm.db"
	defaultShutdownTimeout          = 10 * time.Second
	defaultGitHubAPIBaseURL         = "https://api.github.com/"
	defaultGitHubRequestTimeout     = 15 * time.Second
	defaultGitHubSyncTimeout        = 2 * time.Minute
	defaultGitHubPollInterval       = 5 * time.Minute
	defaultGitHubPollScanInterval   = 15 * time.Second
	defaultGitHubMaxPages           = 10
	defaultGitHubMaxItems           = 500
	defaultGitHubMaxSyncConcurrency = 4
	defaultOpenCodeEndpoint         = "http://localhost:4096"
	defaultOpenCodeAgent            = "prompt-planner"
	defaultOpenCodeUsername         = "opencode"
	defaultOpenCodeTimeout          = 45 * time.Second
	defaultOpenCodeMaxRequestBytes  = 256 << 10
	defaultPromptEvidenceLimit      = 12
	defaultPromptEvidenceChars      = 4000
)

type Config struct {
	Address                   string
	DatabasePath              string
	ShutdownTimeout           time.Duration
	GitHubToken               string
	GitHubAPIBaseURL          string
	GitHubRequestTimeout      time.Duration
	GitHubSyncTimeout         time.Duration
	GitHubDefaultPollInterval time.Duration
	GitHubPollScanInterval    time.Duration
	GitHubMaxPages            int
	GitHubMaxItems            int
	GitHubMaxSyncConcurrency  int
	OpenCodeEnabled           bool
	OpenCodeEndpoint          string
	OpenCodeProvider          string
	OpenCodeModel             string
	OpenCodeAgent             string
	OpenCodeUsername          string
	OpenCodePassword          string
	OpenCodeTimeout           time.Duration
	OpenCodeMaxRequestBytes   int64
	PromptFallbackEnabled     bool
	PromptEvidenceLimit       int
	PromptEvidenceChars       int
}

func Load() (Config, error) {
	shutdownTimeout, err := durationFromEnv("APP_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	requestTimeout, err := durationFromEnv("GITHUB_REQUEST_TIMEOUT", defaultGitHubRequestTimeout)
	if err != nil {
		return Config{}, err
	}
	syncTimeout, err := durationFromEnv("GITHUB_SYNC_TIMEOUT", defaultGitHubSyncTimeout)
	if err != nil {
		return Config{}, err
	}
	pollInterval, err := durationFromEnv("GITHUB_DEFAULT_POLL_INTERVAL", defaultGitHubPollInterval)
	if err != nil {
		return Config{}, err
	}
	pollScanInterval, err := durationFromEnv("GITHUB_POLL_SCAN_INTERVAL", defaultGitHubPollScanInterval)
	if err != nil {
		return Config{}, err
	}
	maxPages, err := positiveIntFromEnv("GITHUB_MAX_PAGES", defaultGitHubMaxPages)
	if err != nil {
		return Config{}, err
	}
	maxItems, err := positiveIntFromEnv("GITHUB_MAX_ITEMS", defaultGitHubMaxItems)
	if err != nil {
		return Config{}, err
	}
	maxConcurrency, err := positiveIntFromEnv("GITHUB_MAX_SYNC_CONCURRENCY", defaultGitHubMaxSyncConcurrency)
	if err != nil {
		return Config{}, err
	}
	openCodeEnabled, err := boolFromEnv("OPENCODE_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	openCodeTimeout, err := durationFromEnv("OPENCODE_TIMEOUT", defaultOpenCodeTimeout)
	if err != nil {
		return Config{}, err
	}
	openCodeMaxRequestBytes, err := positiveInt64FromEnv("OPENCODE_MAX_REQUEST_BYTES", defaultOpenCodeMaxRequestBytes)
	if err != nil {
		return Config{}, err
	}
	fallbackEnabled, err := boolFromEnv("PROMPT_FALLBACK_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	evidenceLimit, err := positiveIntFromEnv("PROMPT_EVIDENCE_LIMIT", defaultPromptEvidenceLimit)
	if err != nil {
		return Config{}, err
	}
	evidenceChars, err := positiveIntFromEnv("PROMPT_EVIDENCE_CHARS", defaultPromptEvidenceChars)
	if err != nil {
		return Config{}, err
	}

	config := Config{
		Address:                   stringFromEnv("APP_ADDR", defaultAddress),
		DatabasePath:              stringFromEnv("APP_DATABASE_PATH", defaultDatabasePath),
		ShutdownTimeout:           shutdownTimeout,
		GitHubToken:               strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		GitHubAPIBaseURL:          stringFromEnv("GITHUB_API_BASE_URL", defaultGitHubAPIBaseURL),
		GitHubRequestTimeout:      requestTimeout,
		GitHubSyncTimeout:         syncTimeout,
		GitHubDefaultPollInterval: pollInterval,
		GitHubPollScanInterval:    pollScanInterval,
		GitHubMaxPages:            maxPages,
		GitHubMaxItems:            maxItems,
		GitHubMaxSyncConcurrency:  maxConcurrency,
		OpenCodeEnabled:           openCodeEnabled,
		OpenCodeEndpoint:          stringFromEnv("OPENCODE_ENDPOINT", defaultOpenCodeEndpoint),
		OpenCodeProvider:          strings.TrimSpace(os.Getenv("OPENCODE_PROVIDER")),
		OpenCodeModel:             strings.TrimSpace(os.Getenv("OPENCODE_MODEL")),
		OpenCodeAgent:             stringFromEnv("OPENCODE_AGENT", defaultOpenCodeAgent),
		OpenCodeUsername:          stringFromEnv("OPENCODE_USERNAME", defaultOpenCodeUsername),
		OpenCodePassword:          strings.TrimSpace(os.Getenv("OPENCODE_PASSWORD")),
		OpenCodeTimeout:           openCodeTimeout,
		OpenCodeMaxRequestBytes:   openCodeMaxRequestBytes,
		PromptFallbackEnabled:     fallbackEnabled,
		PromptEvidenceLimit:       evidenceLimit,
		PromptEvidenceChars:       evidenceChars,
	}
	if config.OpenCodeEnabled && (config.OpenCodeProvider == "" || config.OpenCodeModel == "") {
		return Config{}, fmt.Errorf("OPENCODE_PROVIDER and OPENCODE_MODEL are required when OPENCODE_ENABLED=true")
	}
	if config.PromptEvidenceLimit < 8 {
		return Config{}, fmt.Errorf("PROMPT_EVIDENCE_LIMIT must be at least 8")
	}
	if config.PromptEvidenceChars < 256 {
		return Config{}, fmt.Errorf("PROMPT_EVIDENCE_CHARS must be at least 256")
	}
	return config, nil
}

func stringFromEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("parse %s: duration must be positive", key)
	}
	return duration, nil
}

func positiveIntFromEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("parse %s: value must be a positive integer", key)
	}
	return parsed, nil
}

func positiveInt64FromEnv(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("parse %s: value must be a positive integer", key)
	}
	return parsed, nil
}

func boolFromEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: value must be true or false", key)
	}
	return parsed, nil
}
