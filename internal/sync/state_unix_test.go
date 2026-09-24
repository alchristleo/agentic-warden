//go:build unix

package sync_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/acme/agent-wrapper/internal/sync"
)

// umask is process-wide; this test must not run with t.Parallel and must
// restore the umask it changes, or it would corrupt every other test in the
// process.

func TestSaveStateDirSurvivesAStrictUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	dir := filepath.Join(t.TempDir(), "state")
	if err := sync.SaveState(dir, sync.State{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("state dir mode = %o, want 0755 under umask 077: `aw-sync status` runs as the developer, not root", info.Mode().Perm())
	}
}
