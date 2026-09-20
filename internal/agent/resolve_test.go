package agent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
)

// fakeBinary writes an executable file named name into dir and returns its path.
func fakeBinary(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return path
}

func pathEnv(dirs ...string) []string {
	return []string{"PATH=" + strings.Join(dirs, string(filepath.ListSeparator))}
}

func TestResolveBinaryFindsTheAgentOnPath(t *testing.T) {
	dir := t.TempDir()
	want := fakeBinary(t, dir, "claude")

	got, err := agent.ResolveBinary("claude", pathEnv(dir))
	if err != nil {
		t.Fatalf("ResolveBinary: %v", err)
	}
	if got != want {
		t.Errorf("ResolveBinary() = %q, want %q", got, want)
	}
}

func TestResolveBinarySkipsTheWrapperItselfSoItCannotRecurse(t *testing.T) {
	// The wrapper is commonly installed on PATH under the agent's own name.
	// Standing in for it here is a symlink to the running test binary.
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	wrapperDir := t.TempDir()
	if err := os.Symlink(self, filepath.Join(wrapperDir, "claude")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	realDir := t.TempDir()
	want := fakeBinary(t, realDir, "claude")

	got, err := agent.ResolveBinary("claude", pathEnv(wrapperDir, realDir))
	if err != nil {
		t.Fatalf("ResolveBinary: %v", err)
	}
	if got != want {
		t.Errorf("ResolveBinary() = %q, want the real agent at %q", got, want)
	}
}

func TestResolveBinaryReportsTheDirectoriesItSearched(t *testing.T) {
	dir := t.TempDir()

	_, err := agent.ResolveBinary("claude", pathEnv(dir))
	if err == nil {
		t.Fatal("ResolveBinary() error = nil, want a not-found error")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the searched directory %q", err, dir)
	}
}

func TestResolveBinaryTreatsANameWithASeparatorAsAPath(t *testing.T) {
	dir := t.TempDir()
	want := fakeBinary(t, dir, "claude")

	got, err := agent.ResolveBinary(want, nil)
	if err != nil {
		t.Fatalf("ResolveBinary: %v", err)
	}
	if got != want {
		t.Errorf("ResolveBinary() = %q, want %q", got, want)
	}
}

func TestResolveBinaryRejectsANonExecutableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("not executable"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := agent.ResolveBinary("claude", pathEnv(dir)); err == nil {
		t.Error("ResolveBinary() error = nil, want an error for a non-executable file")
	}
}
