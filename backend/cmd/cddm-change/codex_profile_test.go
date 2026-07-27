package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHostCodexProfilePathRequiresNamedProfileFile(t *testing.T) {
	host := t.TempDir()
	t.Setenv("CODEX_HOME", host)
	profile := filepath.Join(host, "deep-review.config.toml")
	if err := os.WriteFile(profile, []byte("model = \"gpt-test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := hostCodexProfilePath("deep-review")
	if err != nil {
		t.Fatal(err)
	}
	if got != profile {
		t.Fatalf("profile path=%q want=%q", got, profile)
	}
	if _, err := hostCodexProfilePath("missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing profile err=%v", err)
	}
}

func TestCodexProfileShimInjectsProfileAndCopiesWorkerFile(t *testing.T) {
	root := t.TempDir()
	host := filepath.Join(root, "host")
	worker := filepath.Join(root, "worker")
	if err := os.MkdirAll(host, 0o700); err != nil {
		t.Fatal(err)
	}
	profileSource := filepath.Join(host, "deep-review.config.toml")
	profileBody := "model = \"gpt-profile\"\nmodel_reasoning_effort = \"high\"\n"
	if err := os.WriteFile(profileSource, []byte(profileBody), 0o600); err != nil {
		t.Fatal(err)
	}

	argsPath := filepath.Join(root, "args.txt")
	envPath := filepath.Join(root, "env.txt")
	realCodex := filepath.Join(root, "real-codex")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CDDM_TEST_ARGS\"\nenv > \"$CDDM_TEST_ENV\"\n"
	if err := os.WriteFile(realCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv(codexRealEnv, realCodex)
	t.Setenv(codexProfileEnv, "deep-review")
	t.Setenv(codexProfileSourceEnv, profileSource)
	t.Setenv("CODEX_HOME", worker)
	t.Setenv("CDDM_TEST_ARGS", argsPath)
	t.Setenv("CDDM_TEST_ENV", envPath)

	if rc := runCodexProfileShim([]string{"exec", "--json", "-m", "gpt-explicit"}); rc != 0 {
		t.Fatalf("shim rc=%d", rc)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	gotArgs := strings.Fields(string(argsData))
	wantArgs := []string{"exec", "--profile", "deep-review", "--json", "-m", "gpt-explicit"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("argv=%q want=%q", gotArgs, wantArgs)
	}

	copied := filepath.Join(worker, "deep-review.config.toml")
	copiedData, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	if string(copiedData) != profileBody {
		t.Fatalf("copied profile=%q want=%q", copiedData, profileBody)
	}
	info, err := os.Stat(copied)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode=%o want=600", info.Mode().Perm())
	}

	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	envText := string(envData)
	for _, secret := range []string{codexRealEnv + "=", codexProfileEnv + "=", codexProfileSourceEnv + "="} {
		if strings.Contains(envText, secret) {
			t.Fatalf("shim control env leaked to real Codex: %s", secret)
		}
	}
	if !strings.Contains(envText, "CODEX_HOME="+worker) {
		t.Fatalf("worker CODEX_HOME missing from real Codex env")
	}
}

func TestCodexProfileShimLeavesNonExecCommandsUnchanged(t *testing.T) {
	root := t.TempDir()
	argsPath := filepath.Join(root, "args.txt")
	realCodex := filepath.Join(root, "real-codex")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CDDM_TEST_ARGS\"\n"
	if err := os.WriteFile(realCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(codexRealEnv, realCodex)
	t.Setenv(codexProfileEnv, "deep-review")
	t.Setenv(codexProfileSourceEnv, filepath.Join(root, "does-not-exist"))
	t.Setenv("CDDM_TEST_ARGS", argsPath)

	if rc := runCodexProfileShim([]string{"login", "status"}); rc != 0 {
		t.Fatalf("shim rc=%d", rc)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(data)); !reflect.DeepEqual(got, []string{"login", "status"}) {
		t.Fatalf("argv=%q", got)
	}
}

func TestCodexProfileSelectionPersistsAndClears(t *testing.T) {
	repo := t.TempDir()
	e := &engine{repo: repo, issue: 17}
	if err := e.persistCodexProfile("deep-review"); err != nil {
		t.Fatal(err)
	}
	if got := e.loadCodexProfile(); got != "deep-review" {
		t.Fatalf("profile=%q", got)
	}
	info, err := os.Stat(e.codexProfileStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profile state mode=%o want=600", info.Mode().Perm())
	}
	if err := e.selectCodexProfile(""); err != nil {
		t.Fatal(err)
	}
	if got := e.loadCodexProfile(); got != "" {
		t.Fatalf("cleared profile=%q", got)
	}
}
