package sync_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/acme/agent-wrapper/internal/sync"
)

func TestLockIsExclusiveUntilReleased(t *testing.T) {
	dir := t.TempDir()
	release, err := sync.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Lock(dir); !errors.Is(err, sync.ErrLocked) {
		t.Fatalf("second Lock: err = %v, want ErrLocked", err)
	}
	release()
	again, err := sync.Lock(dir)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	again()
}

func TestLockFileIsLeftInPlace(t *testing.T) {
	dir := t.TempDir()
	release, err := sync.Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	release()
	info, err := os.Stat(filepath.Join(dir, sync.LockFile))
	if err != nil {
		t.Fatalf("lock file after release: %v; deleting it would race a concurrent opener", err)
	}
	if info.Mode().Perm()&0o044 == 0 {
		t.Errorf("lock file mode = %v, want world-readable 0644", info.Mode().Perm())
	}
}

func TestLockOnAMissingStateDirIsNotExist(t *testing.T) {
	_, err := sync.Lock(filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}
