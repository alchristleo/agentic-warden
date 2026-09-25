// Package config reads the control plane's configuration from the environment.
//
// Every value has a default that works, and a malformed override is an error
// rather than a silent fallback: a server that quietly ignores the timeout you
// set is worse than one that refuses to start.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
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
	// PublicURL is awd's external https origin. Nil means the admin console
	// is disabled.
	PublicURL *url.URL
	// Console holds the admin console's OIDC settings. Zero when the
	// console is disabled.
	Console ConsoleConfig
}

// ConsoleConfig configures the admin console's OpenID Connect sign-in.
type ConsoleConfig struct {
	// Issuer is the identity provider's OIDC issuer URL.
	Issuer string
	// ClientID is awd's client id at the identity provider.
	ClientID string
	// ClientSecretFile is the path to a file holding the OIDC client secret.
	ClientSecretFile string
	// AdminGroup is the identity-provider group whose members administer
	// the console.
	AdminGroup string
	// UserClaim names the ID token claim that identifies the user; default
	// "email".
	UserClaim string
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

	console := []struct {
		key    string
		target *string
	}{
		{"AWD_CONSOLE_ISSUER", &cfg.Console.Issuer},
		{"AWD_CONSOLE_CLIENT_ID", &cfg.Console.ClientID},
		{"AWD_CONSOLE_CLIENT_SECRET_FILE", &cfg.Console.ClientSecretFile},
		{"AWD_CONSOLE_ADMIN_GROUP", &cfg.Console.AdminGroup},
	}
	publicURL := os.Getenv("AWD_PUBLIC_URL")
	var set, missing []string
	if publicURL != "" {
		set = append(set, "AWD_PUBLIC_URL")
	} else {
		missing = append(missing, "AWD_PUBLIC_URL")
	}
	for _, c := range console {
		*c.target = os.Getenv(c.key)
		if *c.target != "" {
			set = append(set, c.key)
		} else {
			missing = append(missing, c.key)
		}
	}
	cfg.Console.UserClaim = env("AWD_CONSOLE_USER_CLAIM", "email")
	if len(set) > 0 && len(missing) > 0 {
		return Config{}, fmt.Errorf("config: the console needs all of its settings; missing %s", strings.Join(missing, ", "))
	}
	if len(set) > 0 {
		u, err := parsePublicURL(publicURL)
		if err != nil {
			return Config{}, err
		}
		cfg.PublicURL = u
	}
	return cfg, nil
}

// ConsoleEnabled reports whether every console setting is present.
func (c Config) ConsoleEnabled() bool { return c.PublicURL != nil }

// parsePublicURL accepts a bare origin: https anywhere, http only on
// loopback. The redirect URI and the Origin check are both built from it,
// so a path, query or fragment would break one of them silently.
//
// The scheme and host are lowercased and an explicit default port (:443 on
// https, :80 on http) is stripped, so the stored URL's origin equals what
// browsers send in the Origin header on a same-origin request.
func parsePublicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSuffix(raw, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must be an absolute https URL", raw)
	}
	switch {
	case u.Path != "":
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must not have a path", raw)
	case u.RawQuery != "" || u.ForceQuery:
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must not have a query", raw)
	case u.Fragment != "":
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must not have a fragment", raw)
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	loopback := host == "localhost" || host == "127.0.0.1"
	if scheme != "https" && !(scheme == "http" && loopback) {
		return nil, fmt.Errorf("config: AWD_PUBLIC_URL=%q must use https (plain http only on localhost)", raw)
	}
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	u.Scheme = scheme
	u.Host = host
	if port != "" {
		u.Host += ":" + port
	}
	return u, nil
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
