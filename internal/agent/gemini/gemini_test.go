package gemini_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
)

// fakeBinary puts an executable named gemini on a private PATH.
func fakeBinary(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable bit")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gemini")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir, "HOME=" + t.TempDir()}
}

func TestNameIsGemini(t *testing.T) {
	if got := gemini.New().Name(); got != "gemini" {
		t.Errorf("Name = %q", got)
	}
}

func TestBuildPassesArgumentsThroughAndMergesTheEnvironment(t *testing.T) {
	env := fakeBinary(t)

	launch, err := gemini.New().Build(context.Background(), agent.BuildOptions{
		Env:      env,
		Args:     []string{"--model", "gemini-2.5-pro"},
		Settings: agent.Settings{Env: map[string]string{"GOOGLE_GEMINI_BASE_URL": "https://proxy.acme"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if launch.Agent != "gemini" || !strings.HasSuffix(launch.Binary, "/gemini") {
		t.Errorf("launch = %+v", launch)
	}
	if strings.Join(launch.Args, " ") != "--model gemini-2.5-pro" {
		t.Errorf("args = %q; want the developer's arguments untouched", launch.Args)
	}
	found := false
	for _, e := range launch.Env {
		if e == "GOOGLE_GEMINI_BASE_URL=https://proxy.acme" {
			found = true
		}
	}
	if !found {
		t.Errorf("env %q lacks the policy's variable", launch.Env)
	}
	if len(launch.Notes) == 0 || !strings.Contains(strings.Join(launch.Notes, "\n"), "settings.json") {
		t.Errorf("notes %q should say where Gemini's managed settings are enforced", launch.Notes)
	}
}

func TestBuildFailsWhenTheBinaryIsMissing(t *testing.T) {
	_, err := gemini.New().Build(context.Background(), agent.BuildOptions{Env: []string{"PATH=" + t.TempDir()}})

	if err == nil {
		t.Error("Build = nil error; want the missing binary reported")
	}
}
