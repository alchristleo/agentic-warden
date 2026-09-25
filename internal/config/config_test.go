package config_test

import (
	"log/slog"
	"strings"
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

func setConsoleEnv(t *testing.T) {
	t.Setenv("AWD_PUBLIC_URL", "https://awd.example.com")
	t.Setenv("AWD_CONSOLE_ISSUER", "https://idp.example.com")
	t.Setenv("AWD_CONSOLE_CLIENT_ID", "awd")
	t.Setenv("AWD_CONSOLE_CLIENT_SECRET_FILE", "/etc/awd/secret")
	t.Setenv("AWD_CONSOLE_ADMIN_GROUP", "console-admins")
}

func TestConsoleAllUnsetIsOff(t *testing.T) {
	cfg, err := config.FromEnv()
	if err != nil || cfg.ConsoleEnabled() {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestConsoleAllSet(t *testing.T) {
	setConsoleEnv(t)
	cfg, err := config.FromEnv()
	if err != nil || !cfg.ConsoleEnabled() || cfg.Console.UserClaim != "email" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestConsolePartlySetNamesTheMissing(t *testing.T) {
	setConsoleEnv(t)
	t.Setenv("AWD_CONSOLE_CLIENT_ID", "")
	t.Setenv("AWD_CONSOLE_ADMIN_GROUP", "")
	_, err := config.FromEnv()
	if err == nil || !strings.Contains(err.Error(), "AWD_CONSOLE_CLIENT_ID") || !strings.Contains(err.Error(), "AWD_CONSOLE_ADMIN_GROUP") {
		t.Fatalf("err = %v", err)
	}
}

// Review Focus 3. Cases whose want value looks like a URL (has a "://")
// assert the exact normalized form; an empty want only checks there is no
// trailing slash; anything else is a fragment the error must contain.
func TestConsolePublicURLShape(t *testing.T) {
	cases := map[string]string{
		"https://awd.example.com/":      "",
		"http://localhost:8080":         "",
		"http://127.0.0.1:9401":         "",
		"http://awd.example.com":        "https",
		"https://awd.example.com/admin": "path",
		"https://awd.example.com?x=1":   "query",
		"https://awd.example.com#f":     "fragment",
		"awd.example.com":               "https",
		"https://AWD.Example.com":       "https://awd.example.com",
		"https://awd.example.com:443":   "https://awd.example.com",
		"http://localhost:80":           "http://localhost",
		"https://awd.example.com:8443":  "https://awd.example.com:8443",
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			setConsoleEnv(t)
			t.Setenv("AWD_PUBLIC_URL", value)
			cfg, err := config.FromEnv()
			switch {
			case strings.Contains(want, "://"):
				if err != nil || cfg.PublicURL.String() != want {
					t.Fatalf("cfg=%v err=%v, want %q", cfg.PublicURL, err, want)
				}
			case want == "":
				if err != nil || strings.HasSuffix(cfg.PublicURL.String(), "/") {
					t.Fatalf("cfg=%v err=%v", cfg.PublicURL, err)
				}
			default:
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("err = %v, want mention of %q", err, want)
				}
			}
		})
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
