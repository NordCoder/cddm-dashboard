package config

import "testing"

func TestDefaultAddressIsLoopbackOnly(t *testing.T) {
	clearEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "127.0.0.1:8080" {
		t.Fatalf("default address = %q, want loopback-only", cfg.Address)
	}
}
