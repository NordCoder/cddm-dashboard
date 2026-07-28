package githubauth

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Source string

const (
	SourceAnonymous Source = "anonymous"
	SourceEnvironment Source = "environment"
	SourceGitHubCLI Source = "gh_cli"
)

var ErrCLIUnavailable = errors.New("GitHub CLI is unavailable")

type TokenProvider interface {
	Token(context.Context) (string, error)
}

type GHCLI struct{}

func (GHCLI) Token(ctx context.Context) (string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCLIUnavailable, err)
	}
	output, err := exec.CommandContext(ctx, path, "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("read GitHub credential from gh: %w", err)
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("read GitHub credential from gh: empty token")
	}
	return token, nil
}

func Resolve(ctx context.Context, mode, directToken string, provider TokenProvider) (string, Source, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	directToken = strings.TrimSpace(directToken)
	if mode == "" {
		mode = "auto"
	}

	switch mode {
	case "anonymous":
		return "", SourceAnonymous, nil
	case "token":
		if directToken == "" {
			return "", "", fmt.Errorf("GITHUB_TOKEN is required when GITHUB_AUTH_MODE=token")
		}
		return directToken, SourceEnvironment, nil
	case "gh_cli":
		return resolveFromProvider(ctx, provider)
	case "auto":
		if directToken != "" {
			return directToken, SourceEnvironment, nil
		}
		token, source, err := resolveFromProvider(ctx, provider)
		if errors.Is(err, ErrCLIUnavailable) {
			return "", SourceAnonymous, nil
		}
		return token, source, err
	default:
		return "", "", fmt.Errorf("GITHUB_AUTH_MODE must be auto, token, gh_cli, or anonymous")
	}
}

func resolveFromProvider(ctx context.Context, provider TokenProvider) (string, Source, error) {
	if provider == nil {
		return "", "", ErrCLIUnavailable
	}
	token, err := provider.Token(ctx)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", "", fmt.Errorf("GitHub credential provider returned an empty token")
	}
	return strings.TrimSpace(token), SourceGitHubCLI, nil
}
