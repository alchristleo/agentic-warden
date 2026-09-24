//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/sync/timer"
)

func TestRootOnlyRefusesAUserOwnedPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a temp dir is root-owned")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "aw-sync")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := rootOnly(binary)
	if err == nil || !strings.Contains(err.Error(), "is owned by uid") {
		t.Fatalf("rootOnly(%s) = %v; want a refusal naming the owner", binary, err)
	}
	// The first offender found is the binary itself, owned by this user.
	if !strings.Contains(err.Error(), binary) {
		t.Errorf("error %q does not name %s", err, binary)
	}
}

func TestRootOnlyAcceptsRootOwnedSystemPaths(t *testing.T) {
	if err := rootOnly("/"); err != nil {
		t.Errorf("rootOnly(/) = %v", err)
	}
	sh, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Skipf("no /bin/sh: %v", err)
	}
	dir := filepath.Dir(sh)
	for p := dir; ; p = filepath.Dir(p) {
		info, err := os.Stat(p)
		if err != nil {
			t.Skip(err)
		}
		if info.Sys().(*syscall.Stat_t).Uid != 0 || info.Mode().Perm()&0o022 != 0 {
			t.Skipf("%s is not root-only on this host", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	if err := rootOnly(dir); err != nil {
		t.Errorf("rootOnly(%s) = %v", dir, err)
	}
	if err := rootOnly(sh); err != nil {
		t.Errorf("rootOnly(%s) = %v", sh, err)
	}
}

// A root-owned file inside a group- or world-writable directory is still
// replaceable: the directory entry can be swapped.
func TestRootOnlyRefusesAWritableParent(t *testing.T) {
	stat := statOwner
	t.Cleanup(func() { statOwner = stat })
	statOwner = func(path string) (uint32, os.FileMode, error) {
		_, mode, err := stat(path)
		if err != nil {
			return 0, 0, err
		}
		if path == "/usr" {
			return 0, os.ModeDir | 0o775, nil
		}
		return 0, mode, nil
	}
	err := rootOnly("/usr/bin")
	if err == nil || !strings.Contains(err.Error(), "/usr is writable by group or others") {
		t.Errorf("rootOnly = %v; want /usr named as group-writable", err)
	}
}

// A relative --state-dir must reach the unit as an absolute path: the job
// runs from /, where the relative path names a different directory.
func TestTimerParamsMakesARelativeStateDirAbsolute(t *testing.T) {
	cwd := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	p, err := timerParams("/usr/local/bin/aw-sync", "rel/state", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	units, err := timer.Render("linux", p)
	if err != nil {
		t.Fatal(err)
	}
	want := "ExecStart=/usr/local/bin/aw-sync once --state-dir " + filepath.Join(cwd, "rel", "state") + "\n"
	if !strings.Contains(string(units[0].Content), want) {
		t.Errorf("service:\n%s\nwant %q", units[0].Content, want)
	}
}

func TestCheckRootOnlyRefusesAUserOwnedStateDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a temp dir is root-owned")
	}
	sh, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil || rootOnly(sh) != nil {
		t.Skip("no root-only /bin/sh to stand in for the binary")
	}
	dir := t.TempDir()
	err = checkRootOnly("linux", timer.Params{Binary: sh, StateDir: dir, Interval: 5 * time.Minute}, nil)
	if err == nil || !strings.Contains(err.Error(), "with state dir "+dir) || !strings.Contains(err.Error(), dir+" is owned by uid") {
		t.Errorf("err = %v; want a refusal naming the state dir", err)
	}
}

func TestCheckRootOnlyOnlyWarnsOnWindows(t *testing.T) {
	t.Setenv("ProgramFiles", `C:\Program Files`)
	var stderr strings.Builder
	p := timer.Params{Binary: `C:\Users\u\Downloads\aw-sync.exe`, StateDir: `C:\ProgramData\agent-wrapper`, Interval: 5 * time.Minute}
	if err := checkRootOnly("windows", p, &stderr); err != nil {
		t.Fatal(err)
	}
	if want := `warning: C:\Users\u\Downloads\aw-sync.exe is outside Program Files`; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr %q; want %q", stderr.String(), want)
	}
	stderr.Reset()
	p.Binary = `c:\program files\AgentWrapper\aw-sync.exe`
	if err := checkRootOnly("windows", p, &stderr); err != nil || stderr.Len() != 0 {
		t.Errorf("err %v, stderr %q; want silence under Program Files", err, stderr.String())
	}
}
