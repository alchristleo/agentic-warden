//go:build !windows

package claude_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alchristleo/agentic-warden/internal/agent"
	"github.com/alchristleo/agentic-warden/internal/agent/claude"
)

type fakeInfo struct {
	fs.FileInfo
	mode fs.FileMode
	uid  uint32
}

func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) Sys() any           { return &syscall.Stat_t{Uid: f.uid} }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }

// fakeStat reports file as (fileUID, fileMode) and its directory as
// (dirUID, dirMode); every other path stats for real.
func fakeStat(file string, fileUID uint32, fileMode fs.FileMode, dirUID uint32, dirMode fs.FileMode) func(string) (fs.FileInfo, error) {
	return func(p string) (fs.FileInfo, error) {
		real, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		switch p {
		case file:
			return fakeInfo{real, fileMode, fileUID}, nil
		case filepath.Dir(file):
			return fakeInfo{real, fs.ModeDir | dirMode, dirUID}, nil
		}
		return real, nil
	}
}

func doctorHelperFindings(t *testing.T, fileUID uint32, fileMode fs.FileMode, dirUID uint32, dirMode fs.FileMode) (string, []agent.Finding) {
	t.Helper()
	in := newInspection(t)
	helper := in.helperBinary(t)
	in.write(t, filepath.Join(in.systemDir, "managed-settings.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)
	t.Cleanup(claude.SetStat(fakeStat(helper, fileUID, fileMode, dirUID, dirMode)))
	return helper, in.adapter.Inspect([]string{})
}

func TestDoctorHelperOwnershipWarnsOnNonRootOwner(t *testing.T) {
	helper, f := doctorHelperFindings(t, 1000, 0o755, 0, 0o755)
	if !findingsWith(f, agent.Warn, "uid 1000") || !findingsWith(f, agent.Warn, "sudo chown root:root "+helper+" && sudo chmod 755 "+helper) {
		t.Errorf("findings %+v should warn about uid 1000 with the fix", f)
	}
}

func TestDoctorHelperOwnershipWarnsOnGroupWritableHelper(t *testing.T) {
	helper, f := doctorHelperFindings(t, 0, 0o775, 0, 0o755)
	if !findingsWith(f, agent.Warn, helper+" is owned by uid 0 with mode -rwxrwxr-x") {
		t.Errorf("findings %+v should warn about mode 0775", f)
	}
}

func TestDoctorHelperOwnershipWarnsOnWritableDirectory(t *testing.T) {
	helper, f := doctorHelperFindings(t, 0, 0o755, 0, 0o757)
	dir := filepath.Dir(helper)
	if !findingsWith(f, agent.Warn, "sudo chown root:root "+dir+" && sudo chmod 755 "+dir) {
		t.Errorf("findings %+v should warn about the directory", f)
	}
}

func TestDoctorHelperOwnershipCleanForRootOwned(t *testing.T) {
	_, f := doctorHelperFindings(t, 0, 0o755, 0, 0o755)
	if findingsWith(f, agent.Warn, "sudo chown") {
		t.Errorf("findings %+v should not warn", f)
	}
}
