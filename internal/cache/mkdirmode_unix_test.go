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

// ReplaceMode must never re-mode a directory it did not create: aw-sync
// writes into shared directories it does not own (/etc/claude-code,
// --root paths, and so on), and those directories' mode, group ownership
// and ACLs belong to whoever set them up, not to aw-sync.

func TestReplaceModeLeavesAPreexistingDirModeAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "managed-settings.json")
	if err := cache.ReplaceMode(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Errorf("dir mode = %o, want 0750 unchanged: ReplaceMode must not re-mode a directory it did not create", info.Mode().Perm())
	}
}

func TestReplaceModeKeepsSetgidOnAPreexistingDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "managed-settings.json")
	if err := cache.ReplaceMode(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSetgid == 0 {
		t.Error("setgid bit cleared: ReplaceMode must not touch a preexisting directory's mode")
	}
	if info.Mode().Perm() != 0o750 {
		t.Errorf("dir mode = %o, want 0750 unchanged", info.Mode().Perm())
	}
}

func TestReplaceModeOfAPrivateFileLeavesAPreexistingWorldReadableDirAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "machine.json")
	if err := cache.ReplaceMode(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("dir mode = %o, want 0755 unchanged: writing a private file into a shared directory must not tighten it", info.Mode().Perm())
	}
}
