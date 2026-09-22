package gemini_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
)

func findingsWith(findings []agent.Finding, level agent.Level, text string) bool {
	for _, f := range findings {
		if f.Level == level && strings.Contains(f.Message, text) {
			return true
		}
	}
	return false
}

func inspected(t *testing.T) *gemini.Adapter {
	t.Helper()
	a := gemini.New()
	a.SystemDir = t.TempDir()
	return a
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInspectWarnsWhenNeitherFileExists(t *testing.T) {
	findings := inspected(t).Inspect([]string{"PATH=/nowhere"})

	if !findingsWith(findings, agent.Warn, "settings.json") || !findingsWith(findings, agent.Warn, "50-agent-wrapper.toml") {
		t.Errorf("findings %v should warn about both missing files", findings)
	}
}

func TestInspectReportsBothFilesWithTheirRevisions(t *testing.T) {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, gemini.SettingsFile), "{\n  \"admin\": {}\n}\n")
	write(t, filepath.Join(a.SystemDir, gemini.SettingsFile+".aw-revision"), "2026-09-22.7\n")
	write(t, filepath.Join(a.SystemDir, gemini.PoliciesFile), "# Managed by aw-sync from policy revision 2026-09-22.7. Do not edit: the next sync overwrites this file.\n\n")

	findings := a.Inspect([]string{"PATH=/nowhere"})

	ok := 0
	for _, f := range findings {
		if f.Level == agent.OK && strings.Contains(f.Message, "2026-09-22.7") {
			ok++
		}
	}
	if ok != 2 {
		t.Errorf("findings %v should report both files at revision 2026-09-22.7", findings)
	}
}

func TestInspectFlagsUnparseableFiles(t *testing.T) {
	a := inspected(t)
	write(t, filepath.Join(a.SystemDir, gemini.SettingsFile), "{not json")
	write(t, filepath.Join(a.SystemDir, gemini.PoliciesFile), "= not toml")

	findings := a.Inspect([]string{"PATH=/nowhere"})

	if !findingsWith(findings, agent.Error, "settings.json") || !findingsWith(findings, agent.Error, "50-agent-wrapper.toml") {
		t.Errorf("findings %v should flag both files", findings)
	}
}

func TestInspectWarnsWhenTheDeveloperMovedTheSystemSettings(t *testing.T) {
	findings := inspected(t).Inspect([]string{gemini.SystemSettingsEnv + "=/home/dev/mine.json"})

	if !findingsWith(findings, agent.Warn, gemini.SystemSettingsEnv) {
		t.Errorf("findings %v should warn that bare gemini reads the developer's file", findings)
	}
}
