package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/claude"
)

// env is a getenv over a fixed map, so the tests control exactly what the
// helper would see in a developer's shell.
func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestAReleaseBuildReadsTheSystemPathsWhateverTheEnvironmentSays(t *testing.T) {
	cfg, notes := loadConfig(env(map[string]string{
		"AW_POLICY_BUNDLE": "/home/dev/mine.json",
		"AW_POLICY_CONFIG": "/home/dev/mine-config.json",
	}), false)

	want := filepath.Join(claude.SystemDir(runtime.GOOS), claude.BundleFile)
	if cfg.BundlePath != want {
		t.Errorf("BundlePath = %q, want the system path %q; a developer's variable must not move it", cfg.BundlePath, want)
	}
	joined := strings.Join(notes, "\n")
	for _, name := range []string{"AW_POLICY_BUNDLE", "AW_POLICY_CONFIG"} {
		if !strings.Contains(joined, name) || !strings.Contains(joined, "ignores it") {
			t.Errorf("notes %q should tell the developer %s is set and ignored", notes, name)
		}
	}
}

func TestAReleaseBuildIsQuietWhenNothingIsSet(t *testing.T) {
	_, notes := loadConfig(env(nil), false)

	for _, n := range notes {
		if strings.Contains(n, "ignores it") {
			t.Errorf("note %q with no variable set; the note is for a developer who set one", n)
		}
	}
}

func TestATestBuildHonoursTheOverrides(t *testing.T) {
	cfg, _ := loadConfig(env(map[string]string{"AW_POLICY_BUNDLE": "/tmp/fixture.json"}), true)

	if cfg.BundlePath != "/tmp/fixture.json" {
		t.Errorf("BundlePath = %q; a tagged build exists so tests can point at fixtures", cfg.BundlePath)
	}
}

func TestBuildInfoNamesTheSetting(t *testing.T) {
	if got := buildInfo(false); got != "env-overrides=off" {
		t.Errorf("buildInfo(false) = %q", got)
	}
	if got := buildInfo(true); got != "env-overrides=on" {
		t.Errorf("buildInfo(true) = %q", got)
	}
}
