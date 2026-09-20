package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/config"
)

func TestFromEnvSuppliesWorkingDefaults(t *testing.T) {
	got, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if got.Addr == "" {
		t.Error("Addr is empty, want a default listen address")
	}
	if got.ReadTimeout == 0 || got.WriteTimeout == 0 || got.IdleTimeout == 0 {
		t.Errorf("timeouts = %+v, want non-zero defaults so a stuck client cannot hold a connection", got)
	}
	if got.ShutdownTimeout == 0 {
		t.Error("ShutdownTimeout is zero, want a bounded drain period")
	}
}

func TestFromEnvReadsTheListenAddress(t *testing.T) {
	t.Setenv("AWD_ADDR", ":9999")

	got, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if got.Addr != ":9999" {
		t.Errorf("Addr = %q, want %q", got.Addr, ":9999")
	}
}

func TestFromEnvWithoutADatabaseSelectsInMemoryStorage(t *testing.T) {
	t.Setenv("AWD_DATABASE_URL", "")

	got, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if got.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty", got.DatabaseURL)
	}
}

func TestFromEnvReadsTheLogLevel(t *testing.T) {
	t.Setenv("AWD_LOG_LEVEL", "debug")

	got, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if got.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want %v", got.LogLevel, slog.LevelDebug)
	}
}

func TestFromEnvRejectsAnUnknownLogLevel(t *testing.T) {
	t.Setenv("AWD_LOG_LEVEL", "chatty")

	if _, err := config.FromEnv(); err == nil {
		t.Error("FromEnv() error = nil, want an error for an unknown log level")
	}
}

func TestFromEnvReadsADurationOverride(t *testing.T) {
	t.Setenv("AWD_READ_TIMEOUT", "45s")

	got, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if got.ReadTimeout != 45*time.Second {
		t.Errorf("ReadTimeout = %v, want %v", got.ReadTimeout, 45*time.Second)
	}
}

func TestFromEnvRejectsAMalformedDuration(t *testing.T) {
	t.Setenv("AWD_READ_TIMEOUT", "ages")

	_, err := config.FromEnv()

	if err == nil {
		t.Fatal("FromEnv() error = nil, want an error for a malformed duration")
	}
	if !contains(err.Error(), "AWD_READ_TIMEOUT") {
		t.Errorf("error %q does not name the offending variable", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
