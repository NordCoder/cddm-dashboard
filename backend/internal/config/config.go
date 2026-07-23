package config

import (
	"fmt"
	"os"
	"time"
)

const (
	defaultAddress         = ":8080"
	defaultDatabasePath    = "data/cddm.db"
	defaultShutdownTimeout = 10 * time.Second
)

type Config struct {
	Address         string
	DatabasePath    string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	shutdownTimeout, err := durationFromEnv("APP_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Address:         stringFromEnv("APP_ADDR", defaultAddress),
		DatabasePath:    stringFromEnv("APP_DATABASE_PATH", defaultDatabasePath),
		ShutdownTimeout: shutdownTimeout,
	}, nil
}

func stringFromEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
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
