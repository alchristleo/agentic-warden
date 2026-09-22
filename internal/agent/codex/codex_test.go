package codex_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/codex"
)

// fakeBinary puts an executable named codex on a private PATH.
func fakeBinary(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable bit")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir, "HOME=" + t.TempDir()}
}

func TestNameIsCodex(t *testing.T) {
	if got := codex.New().Name(); got != "codex" {
		t.Errorf("Name = %q", got)
	}
}

func TestBuildPassesArgumentsThroughAndMergesTheEnvironment(t *testing.T) {
	env := fakeBinary(t)

	launch, err := codex.New().Build(context.Background(), agent.BuildOptions{
		Env:      env,
		Args:     []string{"--model", "gpt-5"},
		Settings: agent.Settings{Env: map[string]string{"OPENAI_BASE_URL": "https://proxy.acme"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if launch.Agent != "codex" || !strings.HasSuffix(launch.Binary, "/codex") {
		t.Errorf("launch = %+v", launch)
	}
	if strings.Join(launch.Args, " ") != "--model gpt-5" {
		t.Errorf("args = %q; want the developer's arguments untouched", launch.Args)
	}
	found := false
	for _, e := range launch.Env {
		if e == "OPENAI_BASE_URL=https://proxy.acme" {
			found = true
		}
	}
	if !found {
		t.Errorf("env %q lacks the policy's variable", launch.Env)
	}
	if len(launch.Notes) == 0 || !strings.Contains(strings.Join(launch.Notes, "\n"), "requirements.toml") {
		t.Errorf("notes %q should say where Codex's managed settings are enforced", launch.Notes)
	}
}

func TestBuildFailsWhenTheBinaryIsMissing(t *testing.T) {
	_, err := codex.New().Build(context.Background(), agent.BuildOptions{Env: []string{"PATH=" + t.TempDir()}})

	if err == nil {
		t.Error("Build = nil error; want the missing binary reported")
	}
}

func TestBuildTurnsTheLaunchDocumentIntoConfigOverrides(t *testing.T) {
	env := fakeBinary(t)

	launch, err := codex.New().Build(context.Background(), agent.BuildOptions{
		Env:  env,
		Args: []string{"--model", "gpt-5"},
		Settings: agent.Settings{Launch: map[string]any{
			"sandbox_mode":    "read-only",
			"approval_policy": "untrusted",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Sorted overrides first, the developer's arguments last so an explicit
	// -c on the command line wins.
	want := `-c approval_policy="untrusted" -c sandbox_mode="read-only" --model gpt-5`
	if got := strings.Join(launch.Args, " "); got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
	notes := strings.Join(launch.Notes, "\n")
	if !strings.Contains(notes, `-c sandbox_mode="read-only" from launch`) {
		t.Errorf("notes %q should list each override", launch.Notes)
	}
}

func TestBuildWithoutALaunchDocumentInjectsNothing(t *testing.T) {
	env := fakeBinary(t)
	launch, err := codex.New().Build(context.Background(), agent.BuildOptions{Env: env, Args: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(launch.Args, " ") != "x" {
		t.Errorf("args = %q; nothing to inject", launch.Args)
	}
}
