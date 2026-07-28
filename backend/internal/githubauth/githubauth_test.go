package githubauth

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct {
	token string
	err   error
	calls int
}

func (provider *fakeProvider) Token(context.Context) (string, error) {
	provider.calls++
	return provider.token, provider.err
}

func TestResolvePrefersEnvironmentTokenInAutoMode(t *testing.T) {
	provider := &fakeProvider{token: "from-cli"}
	token, source, err := Resolve(context.Background(), "auto", "from-env", provider)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if token != "from-env" || source != SourceEnvironment || provider.calls != 0 {
		t.Fatalf("Resolve() = %q, %q, calls=%d", token, source, provider.calls)
	}
}

func TestResolveUsesGitHubCLIWithoutEnvironmentToken(t *testing.T) {
	provider := &fakeProvider{token: "from-cli"}
	token, source, err := Resolve(context.Background(), "gh_cli", "", provider)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if token != "from-cli" || source != SourceGitHubCLI || provider.calls != 1 {
		t.Fatalf("Resolve() = %q, %q, calls=%d", token, source, provider.calls)
	}
}

func TestResolveAutoFallsBackToAnonymousWhenCLIIsUnavailable(t *testing.T) {
	provider := &fakeProvider{err: ErrCLIUnavailable}
	token, source, err := Resolve(context.Background(), "auto", "", provider)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if token != "" || source != SourceAnonymous {
		t.Fatalf("Resolve() = %q, %q", token, source)
	}
}

func TestResolveDoesNotHideBrokenGitHubCLIAuthentication(t *testing.T) {
	provider := &fakeProvider{err: errors.New("not logged in")}
	_, _, err := Resolve(context.Background(), "auto", "", provider)
	if err == nil {
		t.Fatal("Resolve() error = nil")
	}
}

func TestResolveTokenModeRequiresEnvironmentToken(t *testing.T) {
	_, _, err := Resolve(context.Background(), "token", "", &fakeProvider{})
	if err == nil {
		t.Fatal("Resolve() error = nil")
	}
}

func TestResolveRejectsUnknownMode(t *testing.T) {
	_, _, err := Resolve(context.Background(), "ssh", "", &fakeProvider{})
	if err == nil {
		t.Fatal("Resolve() error = nil")
	}
}
