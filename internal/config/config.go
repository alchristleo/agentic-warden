// Package config reads the control plane's configuration from the environment.
//
// Every value has a default that works, and a malformed override is an error
// rather than a silent fallback: a server that quietly ignores the timeout you
// set is worse than one that refuses to start.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config is the control plane's runtime configuration.
type Config struct {
	// Addr is the listen address.
	Addr string
	// DatabaseURL selects Postgres storage. Empty means in-memory, which does
	// not survive a restart and suits evaluation only.
	DatabaseURL string
	// LogLevel is the minimum level written.
	LogLevel slog.Level

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	// ShutdownTimeout bounds how long in-flight requests may drain.
	ShutdownTimeout time.Duration
	// AdminToken is the bearer token for administrative routes. Unset means
	// those routes are disabled.
	AdminToken string
	// SCIMToken is the bearer token the identity provider presents on
	// /scim/v2. Unset means SCIM is disabled.
	SCIMToken string
}

// FromEnv builds a Config from the environment.
func FromEnv() (Config, error) {
	cfg := Config{
		Addr:        env("AWD_ADDR", ":8080"),
		DatabaseURL: os.Getenv("AWD_DATABASE_URL"),
		AdminToken:  os.Getenv("AWD_ADMIN_TOKEN"),
		SCIMToken:   os.Getenv("AWD_SCIM_TOKEN"),
	}

	level, err := logLevel(env("AWD_LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = level

	durations := []struct {
		key    string
		target *time.Duration
		value  time.Duration
	}{
		{"AWD_READ_TIMEOUT", &cfg.ReadTimeout, 10 * time.Second},
		{"AWD_WRITE_TIMEOUT", &cfg.WriteTimeout, 30 * time.Second},
		{"AWD_IDLE_TIMEOUT", &cfg.IdleTimeout, 120 * time.Second},
		{"AWD_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout, 30 * time.Second},
	}
	for _, d := range durations {
		parsed, err := duration(d.key, d.value)
		if err != nil {
			return Config{}, err
		}
		*d.target = parsed
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a duration: %w", key, raw, err)
	}
	return parsed, nil
}

func logLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("config: AWD_LOG_LEVEL=%q is not one of debug, info, warn, error", raw)
	}
}
