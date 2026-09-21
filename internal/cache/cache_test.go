package cache_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/cache"
)

func TestWriteStoresTheContentAndReturnsItsPath(t *testing.T) {
	dir := t.TempDir()

	path, err := cache.Write(dir, "settings", ".json", []byte(`{"model":"opus"}`))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != `{"model":"opus"}` {
		t.Errorf("content = %q, want the bytes that were written", got)
	}
	if base := filepath.Base(path); !strings.HasPrefix(base, "settings-") || !strings.HasSuffix(base, ".json") {
		t.Errorf("file name %q does not use the given prefix and extension", base)
	}
}

func TestWriteIsContentAddressedSoConcurrentLaunchesCannotClobberEachOther(t *testing.T) {
	dir := t.TempDir()

	same, err := cache.Write(dir, "settings", ".json", []byte(`{"model":"opus"}`))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	again, err := cache.Write(dir, "settings", ".json", []byte(`{"model":"opus"}`))
	if err != nil {
		t.Fatalf("Write again: %v", err)
	}
	different, err := cache.Write(dir, "settings", ".json", []byte(`{"model":"sonnet"}`))
	if err != nil {
		t.Fatalf("Write different: %v", err)
	}

	if same != again {
		t.Errorf("identical content produced %q and %q, want one path", same, again)
	}
	if same == different {
		t.Errorf("different content produced the same path %q", same)
	}
}

func TestWriteCreatesTheDirectoryWhenItIsMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "merge", "claude")

	if _, err := cache.Write(dir, "settings", ".json", []byte("{}")); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func TestWriteLeavesNoTemporaryFilesBehind(t *testing.T) {
	dir := t.TempDir()

	path, err := cache.Write(dir, "settings", ".json", []byte("{}"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || filepath.Join(dir, entries[0].Name()) != path {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only the written file", names)
	}
}

func TestPruneRemovesStaleFilesAndKeepsFreshOnes(t *testing.T) {
	dir := t.TempDir()
	fresh, err := cache.Write(dir, "settings", ".json", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	stale, err := cache.Write(dir, "settings", ".json", []byte(`{"b":2}`))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := cache.Prune(dir, 24*time.Hour); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file was pruned: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file survived Prune, stat err = %v", err)
	}
}

func TestPruneOnAMissingDirectoryIsNotAnError(t *testing.T) {
	if err := cache.Prune(filepath.Join(t.TempDir(), "absent"), time.Hour); err != nil {
		t.Errorf("Prune on a missing directory returned %v, want nil", err)
	}
}

func TestDirIsNamespacedPerAgent(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	claude, err := cache.Dir("claude")
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	codex, err := cache.Dir("codex")
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}

	if claude == codex {
		t.Errorf("Dir() returned %q for both agents, want one directory each", claude)
	}
	if !strings.Contains(claude, "claude") {
		t.Errorf("Dir(%q) = %q, want the agent name in the path", "claude", claude)
	}
}

func TestReplaceSwapsTheFileWhole(t *testing.T) {
	// A reader that opens the file during a write must see either the old
	// content or the new, never a truncated middle.
	path := filepath.Join(t.TempDir(), "nested", "policy.json")

	if err := cache.Replace(path, []byte("first")); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if err := cache.Replace(path, []byte("second")); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want 1: no temp file may be left behind", len(entries))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600: a policy cache is the developer's alone", info.Mode().Perm())
	}
}

func TestReplaceModeWritesAWorldReadableFileInAWorldReadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := filepath.Join(t.TempDir(), "etc", "claude-code")
	path := filepath.Join(dir, "aw-bundle.json")
	if err := cache.ReplaceMode(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %o, want 0644: a developer's helper reads what root wrote", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o755 {
		t.Errorf("dir mode = %o, want 0755: a readable file in an unlistable directory is unreachable", dirInfo.Mode().Perm())
	}
}

func TestReplaceModeKeepsAPrivateFileInAPrivateDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "machine.json")
	if err := cache.ReplaceMode(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}
}

func TestReplaceModeReplacesAnExistingFileWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.json")
	if err := cache.ReplaceMode(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cache.ReplaceMode(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", got, "second")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("directory has %d entries, want 1: no temporary file may be left behind", len(entries))
	}
}
