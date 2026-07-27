package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	codexProfileEnv       = "CDDM_CODEX_PROFILE"
	codexProfileSourceEnv = "CDDM_CODEX_PROFILE_SOURCE"
	codexRealEnv          = "CDDM_CODEX_REAL"
)

func isCodexProfileShim() bool {
	return filepath.Base(os.Args[0]) == "codex" && strings.TrimSpace(os.Getenv(codexRealEnv)) != ""
}

func runCodexProfileShim(args []string) int {
	real := strings.TrimSpace(os.Getenv(codexRealEnv))
	profile := strings.TrimSpace(os.Getenv(codexProfileEnv))
	if real == "" || profile == "" {
		fmt.Fprintln(os.Stderr, "invalid CDDM Codex profile shim environment")
		return 78
	}

	if len(args) > 0 && args[0] == "exec" {
		if err := installProfileIntoWorker(profile); err != nil {
			fmt.Fprintf(os.Stderr, "CDDM Codex profile: %v\n", err)
			return 78
		}
		args = append([]string{"exec", "--profile", profile}, args[1:]...)
	}

	cmd := exec.Command(real, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withoutEnv(os.Environ(), codexProfileEnv, codexProfileSourceEnv, codexRealEnv)
	if err := cmd.Run(); err != nil {
		return exitCode(err)
	}
	return 0
}

func (e *engine) activateCodexProfile(profile string) error {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return nil
	}
	if err := validateProfileName(profile); err != nil {
		return err
	}
	source, err := hostCodexProfilePath(profile)
	if err != nil {
		return err
	}
	real, err := exec.LookPath("codex")
	if err != nil {
		return errors.New("missing required command: codex")
	}
	absReal, err := filepath.Abs(real)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	shimDir := filepath.Join(e.repo, ".worktrees", "runtime", "codex-profile-shims", fmt.Sprintf("issue-%d", e.issue))
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return err
	}
	shim := filepath.Join(shimDir, "codex")
	_ = os.Remove(shim)
	if err := os.Symlink(self, shim); err != nil {
		return fmt.Errorf("create Codex profile shim: %w", err)
	}
	if err := os.Setenv(codexRealEnv, absReal); err != nil {
		return err
	}
	if err := os.Setenv(codexProfileEnv, profile); err != nil {
		return err
	}
	if err := os.Setenv(codexProfileSourceEnv, source); err != nil {
		return err
	}
	return os.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func hostCodexProfilePath(profile string) (string, error) {
	home := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".codex")
	}
	path := filepath.Join(home, profile+".config.toml")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("Codex profile %q not found at %s", profile, path)
		}
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Codex profile %q is not a regular file", profile)
	}
	return path, nil
}

func installProfileIntoWorker(profile string) error {
	source := strings.TrimSpace(os.Getenv(codexProfileSourceEnv))
	workerHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if source == "" || workerHome == "" {
		return errors.New("profile source or worker CODEX_HOME missing")
	}
	if err := os.MkdirAll(workerHome, 0o700); err != nil {
		return err
	}
	destination := filepath.Join(workerHome, profile+".config.toml")
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (e *engine) codexProfileStatePath() string {
	return filepath.Join(e.repo, ".worktrees", "runtime", fmt.Sprintf("issue-%d.codex-profile", e.issue))
}

func (e *engine) persistCodexProfile(profile string) error {
	path := e.codexProfileStatePath()
	if strings.TrimSpace(profile) == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".codex-profile-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(strings.TrimSpace(profile) + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (e *engine) loadCodexProfile() string {
	data, err := os.ReadFile(e.codexProfileStatePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (e *engine) selectCodexProfile(profile string) error {
	profile = strings.TrimSpace(profile)
	if profile != "" {
		if err := e.activateCodexProfile(profile); err != nil {
			return err
		}
	}
	return e.persistCodexProfile(profile)
}

func withoutEnv(env []string, keys ...string) []string {
	blocked := map[string]bool{}
	for _, key := range keys {
		blocked[key] = true
	}
	out := make([]string, 0, len(env))
	for _, item := range env {
		key := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			key = item[:i]
		}
		if !blocked[key] {
			out = append(out, item)
		}
	}
	return out
}
