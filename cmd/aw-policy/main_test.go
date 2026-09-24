package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestWriteNotesCapsAtACompleteRuneNotAByte(t *testing.T) {
	// "aw-policy: " is 11 bytes, an odd length, so a note built entirely of
	// the 2-byte rune "é" has its pairs land on odd offsets: 11, 13, 15...
	// That includes offset maxStderr-1 (16383, odd), so a naive
	// text[:maxStderr] byte cut lands inside that pair and would split it,
	// leaving invalid UTF-8 on stderr. The repeat count only needs to reach
	// past maxStderr; the split happens at the cut point regardless of how
	// far past.
	note := strings.Repeat("é", 8300)
	var buf bytes.Buffer

	writeNotes(&buf, []string{note})

	out := buf.Bytes()
	if len(out) > maxStderr {
		t.Fatalf("len(out) = %d, want <= maxStderr (%d)", len(out), maxStderr)
	}
	if !utf8.Valid(out) {
		t.Fatalf("out is not valid UTF-8: %q", out)
	}
	trimmed := bytes.TrimSuffix(out, []byte("\n"))
	r, size := utf8.DecodeLastRune(trimmed)
	if r != 'é' || size != 2 {
		t.Errorf("out does not end on a complete 'é': last rune = %q, size = %d", r, size)
	}
}

func TestWriteNotesLeavesAShortNoteUnchanged(t *testing.T) {
	var buf bytes.Buffer

	writeNotes(&buf, []string{"hello"})

	if got, want := buf.String(), "aw-policy: hello\n"; got != want {
		t.Errorf("writeNotes output = %q, want %q", got, want)
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
