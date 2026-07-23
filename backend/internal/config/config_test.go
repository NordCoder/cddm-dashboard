package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ADDR", "")
	t.Setenv("APP_DATABASE_PATH", "")
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Address != defaultAddress {
		t.Fatalf("Address = %q, want %q", cfg.Address, defaultAddress)
	}
	if cfg.DatabasePath != defaultDatabasePath {
		t.Fatalf("DatabasePath = %q, want %q", cfg.DatabasePath, defaultDatabasePath)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, defaultShutdownTimeout)
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv("APP_ADDR", "127.0.0.1:9000")
	t.Setenv("APP_DATABASE_PATH", "/tmp/cddm.db")
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "3s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Address != "127.0.0.1:9000" {
		t.Fatalf("Address = %q", cfg.Address)
	}
	if cfg.DatabasePath != "/tmp/cddm.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("ShutdownTimeout = %s", cfg.ShutdownTimeout)
	}
}

func TestLoadRejectsInvalidShutdownTimeout(t *testing.T) {
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "soon")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}
