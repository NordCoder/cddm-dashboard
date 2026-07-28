package config

import (
	"net"
	"testing"
)

func TestDefaultAddressIsLoopbackOnly(t *testing.T) {
	clearEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		t.Fatalf("parse default address %q: %v", cfg.Address, err)
	}
	if host != "127.0.0.1" || port != "1337" {
		t.Fatalf("default address = %q, want loopback 127.0.0.1:1337", cfg.Address)
	}
}
