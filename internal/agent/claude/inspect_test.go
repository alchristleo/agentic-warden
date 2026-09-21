package claude_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
)

// inspection sets up a managed system directory and a Claude config
// directory under the test's control and returns an adapter aimed at them.
type inspection struct {
	adapter   *claude.Adapter
	systemDir string
	configDir string
}

func newInspection(t *testing.T) *inspection {
	t.Helper()
	a := claude.New()
	a.SystemDir = t.TempDir()
	a.ConfigDir = t.TempDir()
	return &inspection{adapter: a, systemDir: a.SystemDir, configDir: a.ConfigDir}
}

func (in *inspection) write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (in *inspection) helperBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aw-policy")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func findingsWith(findings []agent.Finding, level agent.Level, text string) bool {
	for _, f := range findings {
		if f.Level == level && strings.Contains(f.Message, text) {
			return true
		}
	}
	return false
}

func TestInspectReportsAWorkingPolicyHelper(t *testing.T) {
	in := newInspection(t)
	helper := in.helperBinary(t)
	in.write(t, filepath.Join(in.systemDir, "managed-settings.d", "50-agent-wrapper.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.OK, helper) {
		t.Errorf("findings %v should confirm the helper at %s", findings, helper)
	}
	if !findingsWith(findings, agent.OK, "50-agent-wrapper.json") {
		t.Errorf("findings %v should name the file that configures it", findings)
	}
}

func TestInspectWarnsWhenNoPolicyHelperIsConfigured(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "managed-settings.json"), `{"model":"opus"}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "no policyHelper") {
		t.Errorf("findings %v should warn that bare `claude` is ungoverned", findings)
	}
}

func TestInspectFailsWhenTheHelperPathIsNotExecutable(t *testing.T) {
	// This is the configuration that makes Claude Code refuse to start on
	// every launch: worth the loudest level doctor has.
	in := newInspection(t)
	missing := filepath.Join(t.TempDir(), "aw-policy")
	in.write(t, filepath.Join(in.systemDir, "managed-settings.json"),
		`{"policyHelper":{"path":"`+missing+`"}}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Error, missing) {
		t.Errorf("findings %v should report the missing helper as an error", findings)
	}
}

func TestInspectDetectsServerManagedSettingsShadowingTheHelper(t *testing.T) {
	in := newInspection(t)
	helper := in.helperBinary(t)
	in.write(t, filepath.Join(in.systemDir, "managed-settings.json"),
		`{"policyHelper":{"path":"`+helper+`"}}`)
	in.write(t, filepath.Join(in.configDir, "remote-settings.json"), `{"model":"sonnet"}`)

	findings := in.adapter.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "remote-settings.json") {
		t.Errorf("findings %v should warn that a server-managed payload shadows the helper", findings)
	}
}

func TestInspectHonoursTheConfigDirFromTheEnvironment(t *testing.T) {
	in := newInspection(t)
	in.adapter.ConfigDir = ""
	other := t.TempDir()
	in.write(t, filepath.Join(other, "remote-settings.json"), `{"model":"sonnet"}`)

	findings := in.adapter.Inspect([]string{"CLAUDE_CONFIG_DIR=" + other, "HOME=" + t.TempDir()})

	if !findingsWith(findings, agent.Warn, "remote-settings.json") {
		t.Errorf("findings %v should look in CLAUDE_CONFIG_DIR", findings)
	}
}

func TestInspectNotesWhenTheServerManagedFetchIsSkipped(t *testing.T) {
	// A custom base URL makes Claude Code skip the server-managed fetch, so
	// a stale remote-settings.json cannot shadow the helper on this machine.
	in := newInspection(t)
	in.write(t, filepath.Join(in.configDir, "remote-settings.json"), `{"model":"sonnet"}`)

	findings := in.adapter.Inspect([]string{"ANTHROPIC_BASE_URL=https://gateway.example.com"})

	if !findingsWith(findings, agent.OK, "ANTHROPIC_BASE_URL") {
		t.Errorf("findings %v should note that the fetch is skipped", findings)
	}
	if findingsWith(findings, agent.Warn, "remote-settings.json") {
		t.Errorf("findings %v should not warn about shadowing when the fetch is skipped", findings)
	}
}

func TestInspectAnEmptyRemoteSettingsCacheDoesNotShadow(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.configDir, "remote-settings.json"), `{}`)

	findings := in.adapter.Inspect(nil)

	if findingsWith(findings, agent.Warn, "remote-settings.json") {
		t.Errorf("findings %v: an empty payload delivers no policy key and shadows nothing", findings)
	}
}

func TestInspectWarnsWhenThereIsNoBundle(t *testing.T) {
	in := newInspection(t)

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.Warn, "aw-bundle.json") {
		t.Errorf("findings %+v should warn that no bundle has been synced", findings)
	}
}

func TestInspectReportsTheBundleVersion(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "aw-bundle.json"),
		`{"version":"2026-09-21.1","groups":["platform"],"rules":[{"name":"baseline","agents":{"claude":{"managed":{"model":"opus"}}}}]}`)

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.OK, "version 2026-09-21.1") || !findingsWith(findings, agent.OK, "1 rule") {
		t.Errorf("findings %+v should report the bundle's version and rule count", findings)
	}
}

func TestInspectWarnsWhenTheBundleHasNoRules(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "aw-bundle.json"), `{"version":"v1","groups":[],"rules":[]}`)

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.Warn, "no rules") {
		t.Errorf("findings %+v should warn that a rule-less bundle enforces nothing", findings)
	}
}

func TestInspectFlagsAnUnparseableBundle(t *testing.T) {
	in := newInspection(t)
	in.write(t, filepath.Join(in.systemDir, "aw-bundle.json"), "{not json")

	findings := in.adapter.Inspect([]string{})

	if !findingsWith(findings, agent.Error, "not valid JSON") {
		t.Errorf("findings %+v should flag the bundle aw-policy cannot read", findings)
	}
}
