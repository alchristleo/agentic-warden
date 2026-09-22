package gemini_test

import (
	"context"
	"encoding/json"
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

// newAdapter is an adapter whose cache is a temporary directory.
func newAdapter(t *testing.T) *gemini.Adapter {
	t.Helper()
	a := gemini.New()
	a.CacheDir = t.TempDir()
	return a
}

func envValue(env []string, name string) string {
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			return strings.TrimPrefix(e, name+"=")
		}
	}
	return ""
}

func TestBuildWritesTheCompiledSettingsAndPinsThePath(t *testing.T) {
	env := fakeBinary(t)

	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env:  env,
		Args: []string{"--model", "gemini-2.5-pro"},
		Settings: agent.Settings{
			Managed: map[string]any{
				"settings": map[string]any{"admin": map[string]any{"secureModeEnabled": true}},
				"policies": []any{map[string]any{"toolName": "run_shell_command", "decision": "deny", "priority": float64(100)}},
			},
			Env: map[string]string{"GOOGLE_GEMINI_BASE_URL": "https://proxy.acme"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if launch.Agent != "gemini" || !strings.HasSuffix(launch.Binary, "/gemini") {
		t.Errorf("launch = %+v", launch)
	}
	if strings.Join(launch.Args, " ") != "--model gemini-2.5-pro" {
		t.Errorf("args = %q; Gemini takes no settings flag, so the arguments pass through", launch.Args)
	}
	if envValue(launch.Env, "GOOGLE_GEMINI_BASE_URL") != "https://proxy.acme" {
		t.Errorf("env %q lacks the policy's variable", launch.Env)
	}

	settingsPath := envValue(launch.Env, gemini.SystemSettingsEnv)
	if settingsPath == "" {
		t.Fatalf("env %q does not pin %s", launch.Env, gemini.SystemSettingsEnv)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("%s: %v", settingsPath, err)
	}
	if admin, _ := settings["admin"].(map[string]any); admin["secureModeEnabled"] != true {
		t.Errorf("settings = %v; the compiled settings were not written", settings)
	}
	paths, _ := settings["policyPaths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("policyPaths = %v; want the generated policy file", paths)
	}
	policies, err := os.ReadFile(paths[0].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(policies), "# Written by aw for one session") || !strings.Contains(string(policies), "priority = 100\n") {
		t.Errorf("policy file =\n%s", policies)
	}
	if len(launch.Files) != 2 {
		t.Errorf("Files = %v; want the settings and policy files", launch.Files)
	}
}

func TestBuildWithoutPoliciesWritesSettingsAlone(t *testing.T) {
	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env:      fakeBinary(t),
		Settings: agent.Settings{Managed: map[string]any{"settings": map[string]any{"general": map[string]any{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(envValue(launch.Env, gemini.SystemSettingsEnv))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "policyPaths") || len(launch.Files) != 1 {
		t.Errorf("settings = %s, files = %v; no policies means no policy file and no policyPaths", raw, launch.Files)
	}
}

func TestBuildWithNoManagedDocumentLeavesTheSystemSettingsAlone(t *testing.T) {
	// With no rule in scope for Gemini there is nothing of the
	// organization's to pin. Pinning an empty settings file would hide a
	// system settings.json installed by other means, so Build must leave
	// the variable, and the developer's own environment, untouched.
	env := append(fakeBinary(t), "SOME_DEV_VAR=kept")

	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}

	if got := envValue(launch.Env, gemini.SystemSettingsEnv); got != "" {
		t.Errorf("%s = %q, want unset", gemini.SystemSettingsEnv, got)
	}
	if got := envValue(launch.Env, "SOME_DEV_VAR"); got != "kept" {
		t.Errorf("SOME_DEV_VAR = %q, want the developer's own value kept", got)
	}
	if len(launch.Files) != 0 {
		t.Errorf("Files = %v, want none written", launch.Files)
	}
	if !strings.Contains(strings.Join(launch.Notes, "\n"), "the system settings stay as installed") {
		t.Errorf("notes %v do not say the system settings were left alone", launch.Notes)
	}
}

func TestBuildReplacesTheDevelopersOwnSettingsPathAndSaysSo(t *testing.T) {
	// The pin only replaces the developer's value when a managed document
	// is actually in scope; with none, Build leaves the path alone (see
	// TestBuildWithNoManagedDocumentLeavesTheSystemSettingsAlone).
	env := append(fakeBinary(t), gemini.SystemSettingsEnv+"=/home/dev/mine.json")

	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env:      env,
		Settings: agent.Settings{Managed: map[string]any{"settings": map[string]any{"general": map[string]any{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := envValue(launch.Env, gemini.SystemSettingsEnv); got == "/home/dev/mine.json" || got == "" {
		t.Errorf("%s = %q; the pin must replace the developer's value", gemini.SystemSettingsEnv, got)
	}
	if !strings.Contains(strings.Join(launch.Notes, "\n"), "/home/dev/mine.json") {
		t.Errorf("notes %q should say what was replaced", launch.Notes)
	}
	count := 0
	for _, e := range launch.Env {
		if strings.HasPrefix(e, gemini.SystemSettingsEnv+"=") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("env has %d copies of %s; want one", count, gemini.SystemSettingsEnv)
	}
}

func TestBuildRejectsAManagedDocumentThatFailsValidation(t *testing.T) {
	_, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env:      fakeBinary(t),
		Settings: agent.Settings{Managed: map[string]any{"settings": map[string]any{"toolz": 1}}},
	})
	if err == nil || !strings.Contains(err.Error(), `"toolz"`) {
		t.Errorf("err = %v; want the unknown key named", err)
	}
}

func TestBuildRefusesWhenPolicyPathsIsNotAList(t *testing.T) {
	_, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env: fakeBinary(t),
		Settings: agent.Settings{Managed: map[string]any{
			"settings": map[string]any{"policyPaths": "somefile"},
			"policies": []any{map[string]any{"toolName": "*", "decision": "deny", "priority": float64(1)}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "policyPaths") {
		t.Errorf("err = %v; want policyPaths named", err)
	}
}

func TestBuildKeepsTheAuthorsExistingPolicyPathsFirst(t *testing.T) {
	launch, err := newAdapter(t).Build(context.Background(), agent.BuildOptions{
		Env: fakeBinary(t),
		Settings: agent.Settings{Managed: map[string]any{
			"settings": map[string]any{"policyPaths": []any{"/etc/x.toml"}},
			"policies": []any{map[string]any{"toolName": "*", "decision": "deny", "priority": float64(1)}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(envValue(launch.Env, gemini.SystemSettingsEnv))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	paths, _ := settings["policyPaths"].([]any)
	if len(paths) != 2 || paths[0] != "/etc/x.toml" {
		t.Errorf("policyPaths = %v; want the author's entry first and the generated file second", paths)
	}
}

func TestBuildFailsWhenTheBinaryIsMissing(t *testing.T) {
	_, err := gemini.New().Build(context.Background(), agent.BuildOptions{Env: []string{"PATH=" + t.TempDir()}})

	if err == nil {
		t.Error("Build = nil error; want the missing binary reported")
	}
}
