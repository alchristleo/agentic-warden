//go:build unix

package cache_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/acme/agent-wrapper/internal/cache"
)

// umask is process-wide, so these tests set and restore it around each case
// rather than run alongside anything else; none may call t.Parallel().

func TestMkdirModeIgnoresAStrictUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := cache.MkdirMode(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %o, want 0755: MkdirMode must not let umask 077 narrow the leaf directory", info.Mode().Perm())
	}
}

func TestReplaceModeWorldReadableDirSurvivesAStrictUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "aw-bundle.json")
	if err := cache.ReplaceMode(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("dir mode = %o, want 0755 even under umask 077: a developer reading a root-written world-readable file must be able to list its directory", info.Mode().Perm())
	}
}
