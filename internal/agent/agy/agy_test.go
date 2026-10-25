package agy_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/agent"
	"github.com/alchristleo/agentic-warden/internal/agent/agy"
)

func fakeBinary(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable bit")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agy"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir, "HOME=" + t.TempDir()}
}

func TestNameIsAgy(t *testing.T) {
	if got := agy.New().Name(); got != "agy" {
		t.Errorf("Name = %q", got)
	}
}

func TestSystemDirPerOS(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	cases := map[string]string{
		"linux":   "/etc/antigravity",
		"darwin":  "/Library/Application Support/Antigravity",
		"windows": `C:\ProgramData\Antigravity`,
	}
	for goos, want := range cases {
		if got := agy.SystemDir(goos); got != want {
			t.Errorf("SystemDir(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestBuildMergesEnvPassesArgsAndWritesNothing(t *testing.T) {
	env := append(fakeBinary(t), "KEEP=1")

	launch, err := agy.New().Build(context.Background(), agent.BuildOptions{
		Env:      env,
		Args:     []string{"-p", "hello", "--flag"},
		Settings: agent.Settings{Env: map[string]string{"TEAM": "platform"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Agent != "agy" || !strings.HasSuffix(launch.Binary, "agy") {
		t.Errorf("launch = %+v", launch)
	}
	if !reflect.DeepEqual(launch.Args, []string{"-p", "hello", "--flag"}) {
		t.Errorf("args = %v", launch.Args)
	}
	got := map[string]bool{}
	for _, e := range launch.Env {
		got[e] = true
	}
	if !got["TEAM=platform"] || !got["KEEP=1"] {
		t.Errorf("env %v lacks the merged values", launch.Env)
	}
	if len(launch.Files) != 0 {
		t.Errorf("files = %v, want none", launch.Files)
	}
	want := "agy: enforcement is machine-wide (admin_settings.json via aw-sync); nothing is pinned per launch"
	found := false
	for _, n := range launch.Notes {
		found = found || n == want
	}
	if !found {
		t.Errorf("notes %v lack %q", launch.Notes, want)
	}
}

func TestBuildFailsWhenTheBinaryIsMissing(t *testing.T) {
	_, err := agy.New().Build(context.Background(), agent.BuildOptions{Env: []string{"PATH=" + t.TempDir()}})
	if err == nil {
		t.Fatal("want an error")
	}
}
