package claude_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
)

// harness builds an adapter whose every input is under the test's control:
// a fake binary on PATH, a fake home, a fake project, a scratch cache.
type harness struct {
	adapter *claude.Adapter
	env     []string
	home    string
	project string
	binary  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	binDir, home, project, cacheDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()

	binary := filepath.Join(binDir, "claude")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	a := claude.New()
	a.UserSettingsPath = filepath.Join(home, ".claude", "settings.json")
	a.ProjectDir = project
	a.CacheDir = cacheDir

	return &harness{
		adapter: a,
		env:     []string{"PATH=" + binDir, "HOME=" + home},
		home:    home,
		project: project,
		binary:  binary,
	}
}

func (h *harness) writeUserSettings(t *testing.T, body string) {
	t.Helper()
	h.writeFile(t, filepath.Join(h.home, ".claude", "settings.json"), body)
}

func (h *harness) writeProjectSettings(t *testing.T, name, body string) {
	t.Helper()
	h.writeFile(t, filepath.Join(h.project, ".claude", name), body)
}

func (h *harness) writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func (h *harness) build(t *testing.T, o agent.BuildOptions) *agent.Launch {
	t.Helper()
	if o.Env == nil {
		o.Env = h.env
	}
	launch, err := h.adapter.Build(context.Background(), o)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return launch
}

// settingsFlagValue returns the path passed via --settings.
func settingsFlagValue(t *testing.T, args []string) string {
	t.Helper()
	for i, arg := range args {
		if arg == "--settings" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("no --settings flag in args %q", args)
	return ""
}

func mergedSettings(t *testing.T, launch *agent.Launch) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(settingsFlagValue(t, launch.Args))
	if err != nil {
		t.Fatalf("read merged settings: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("merged settings are not valid JSON: %v", err)
	}
	return out
}

func TestLocateFindsTheRealAgent(t *testing.T) {
	h := newHarness(t)

	got, err := h.adapter.Locate(h.env)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if got != h.binary {
		t.Errorf("Locate() = %q, want %q", got, h.binary)
	}
}

func TestBuildPassesTheMergedSettingsFileToTheAgent(t *testing.T) {
	h := newHarness(t)

	launch := h.build(t, agent.BuildOptions{
		Settings: agent.Settings{Managed: map[string]any{"model": "opus"}},
	})

	if got := mergedSettings(t, launch)["model"]; got != "opus" {
		t.Errorf("merged model = %v, want %q", got, "opus")
	}
	if len(launch.Files) == 0 {
		t.Error("Files is empty, want the generated settings file recorded")
	}
}

func TestBuildLayersProjectOverUserAndManagedOverBoth(t *testing.T) {
	h := newHarness(t)
	h.writeUserSettings(t, `{"model":"haiku","cleanupPeriodDays":7}`)
	h.writeProjectSettings(t, "settings.json", `{"model":"sonnet"}`)

	launch := h.build(t, agent.BuildOptions{
		Settings: agent.Settings{Managed: map[string]any{"model": "opus"}},
	})

	merged := mergedSettings(t, launch)
	if merged["model"] != "opus" {
		t.Errorf("model = %v, want managed settings to win", merged["model"])
	}
	if merged["cleanupPeriodDays"] != float64(7) {
		t.Errorf("cleanupPeriodDays = %v, want the user's 7 to survive", merged["cleanupPeriodDays"])
	}
}

func TestBuildLayersLocalProjectSettingsOverSharedProjectSettings(t *testing.T) {
	h := newHarness(t)
	h.writeProjectSettings(t, "settings.json", `{"model":"sonnet"}`)
	h.writeProjectSettings(t, "settings.local.json", `{"model":"haiku"}`)

	launch := h.build(t, agent.BuildOptions{})

	if got := mergedSettings(t, launch)["model"]; got != "haiku" {
		t.Errorf("model = %v, want the local project file to win", got)
	}
}

func TestBuildUnionsPermissionListsAcrossEveryLayer(t *testing.T) {
	h := newHarness(t)
	h.writeUserSettings(t, `{"permissions":{"allow":["WebSearch"]}}`)
	h.writeProjectSettings(t, "settings.json", `{"permissions":{"allow":["Bash(npm run *)"]}}`)

	launch := h.build(t, agent.BuildOptions{
		Settings: agent.Settings{Managed: map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(git status)"}},
		}},
	})

	permissions, _ := mergedSettings(t, launch)["permissions"].(map[string]any)
	allow, _ := permissions["allow"].([]any)
	want := []any{"WebSearch", "Bash(npm run *)", "Bash(git status)"}
	if !reflect.DeepEqual(allow, want) {
		t.Errorf("permissions.allow = %v, want %v", allow, want)
	}
}

func TestBuildPutsTheDevelopersArgumentsLastSoTheyStillWin(t *testing.T) {
	h := newHarness(t)

	launch := h.build(t, agent.BuildOptions{Args: []string{"--model", "opus", "-p", "hello"}})

	tail := launch.Args[len(launch.Args)-4:]
	want := []string{"--model", "opus", "-p", "hello"}
	if !reflect.DeepEqual(tail, want) {
		t.Errorf("args tail = %q, want %q", tail, want)
	}
}

func TestBuildInjectsEnvironmentDefaultsWithoutOverridingTheDeveloper(t *testing.T) {
	h := newHarness(t)
	env := append(h.env, "ANTHROPIC_BASE_URL=https://personal.example")

	launch := h.build(t, agent.BuildOptions{
		Env: env,
		Settings: agent.Settings{Env: map[string]string{
			"ANTHROPIC_BASE_URL":           "https://gateway.acme.com",
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
		}},
	})

	if !contains(launch.Env, "ANTHROPIC_BASE_URL=https://personal.example") {
		t.Error("the developer's ANTHROPIC_BASE_URL was replaced without ForceEnv")
	}
	if !contains(launch.Env, "CLAUDE_CODE_ENABLE_TELEMETRY=1") {
		t.Error("CLAUDE_CODE_ENABLE_TELEMETRY was not injected")
	}
}

func TestBuildForcesEnvironmentDefaultsWhenPolicyRequiresIt(t *testing.T) {
	h := newHarness(t)
	env := append(h.env, "ANTHROPIC_BASE_URL=https://personal.example")

	launch := h.build(t, agent.BuildOptions{
		Env: env,
		Settings: agent.Settings{
			Env:      map[string]string{"ANTHROPIC_BASE_URL": "https://gateway.acme.com"},
			ForceEnv: true,
		},
	})

	if !contains(launch.Env, "ANTHROPIC_BASE_URL=https://gateway.acme.com") {
		t.Errorf("env = %q, want the org gateway to be forced", launch.Env)
	}
	if contains(launch.Env, "ANTHROPIC_BASE_URL=https://personal.example") {
		t.Error("the developer's value survived a forced override")
	}
}

func TestBuildRecordsTheLayerOrderItApplied(t *testing.T) {
	h := newHarness(t)
	h.writeUserSettings(t, `{"model":"haiku"}`)

	launch := h.build(t, agent.BuildOptions{
		Settings: agent.Settings{Managed: map[string]any{"model": "opus"}},
	})

	joined := strings.Join(launch.Notes, "\n")
	for _, want := range []string{"user", "managed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes do not mention the %q layer:\n%s", want, joined)
		}
	}
}

func TestBuildSurvivesCorruptUserSettingsAndSaysSo(t *testing.T) {
	h := newHarness(t)
	h.writeUserSettings(t, `{"model": `)

	launch := h.build(t, agent.BuildOptions{
		Settings: agent.Settings{Managed: map[string]any{"model": "opus"}},
	})

	if got := mergedSettings(t, launch)["model"]; got != "opus" {
		t.Errorf("model = %v, want the launch to proceed on the managed value", got)
	}
	if !strings.Contains(strings.Join(launch.Notes, "\n"), "could not be read") {
		t.Errorf("notes do not report the unreadable file:\n%s", strings.Join(launch.Notes, "\n"))
	}
}

func TestBuildWorksWhenNoSettingsFileExists(t *testing.T) {
	h := newHarness(t)

	launch := h.build(t, agent.BuildOptions{})

	if launch.Binary != h.binary {
		t.Errorf("Binary = %q, want %q", launch.Binary, h.binary)
	}
	if got := mergedSettings(t, launch); len(got) != 0 {
		t.Errorf("merged settings = %v, want an empty document", got)
	}
}

func TestBuildFailsWhenTheAgentIsNotInstalled(t *testing.T) {
	h := newHarness(t)

	_, err := h.adapter.Build(context.Background(), agent.BuildOptions{Env: []string{"PATH=" + t.TempDir()}})

	if err == nil {
		t.Error("Build() error = nil, want an error when claude is not on PATH")
	}
}

func contains(entries []string, want string) bool {
	for _, entry := range entries {
		if entry == want {
			return true
		}
	}
	return false
}
