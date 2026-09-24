//go:build unix

package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
	"github.com/acme/agent-wrapper/internal/sync"
)

// umask is process-wide; this test must not run with t.Parallel and must
// restore the umask it changes, or it would corrupt every other test in the
// process.

// TestRunLeavesAgentRootsWorldReadableUnderAStrictUmask pins the fix for a
// render-order hazard: cache.MkdirMode only forces the mode of the leaf
// directory it creates, so when a renderer's first file lives in a nested
// directory, MkdirAll creates the agent root itself as an ordinary,
// umask-filtered parent. Both shipped renderers avoid this by listing their
// root-level file first (see the comment beside each renderer's file list),
// so this cycle must leave every root and every nested directory the
// renderers create at 0755 even under a hostile umask.
func TestRunLeavesAgentRootsWorldReadableUnderAStrictUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	f := newFakeAwd(t, testBundle())
	reg := &agent.Registry{}
	if err := reg.Register(&claude.Adapter{GOOS: "linux"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&gemini.Adapter{}); err != nil {
		t.Fatal(err)
	}
	cfg, claudeRoot := enrolled(t, f, reg, "claude", "gemini")
	geminiRoot := filepath.Join(t.TempDir(), "gemini-root")
	cfg.Roots["gemini"] = geminiRoot

	res := sync.Run(context.Background(), cfg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	dirs := []string{
		claudeRoot,
		filepath.Join(claudeRoot, "managed-settings.d"),
		geminiRoot,
		filepath.Join(geminiRoot, "policies"),
	}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %o, want 0755 under umask 077", dir, info.Mode().Perm())
		}
	}
}

// TestRunWritesTheAuditLogWorldReadableUnderAStrictUmask pins EnsureStateDir's
// documented promise that the audit log is world-readable: OpenFile's mode
// argument is filtered by the umask on creation like any other mode, so a
// strict umask must not be allowed to leave a freshly created audit log at
// 0600.
func TestRunWritesTheAuditLogWorldReadableUnderAStrictUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	f := newFakeAwd(t, testBundle())
	cfg, _ := enrolled(t, f, claudeRegistry(t), "claude")

	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}

	info, err := os.Stat(filepath.Join(cfg.StateDir, sync.AuditFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("audit log mode = %o, want 0644 under umask 077", info.Mode().Perm())
	}
}
