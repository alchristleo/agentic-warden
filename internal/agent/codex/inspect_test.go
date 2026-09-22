package codex_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/codex"
)

func findingsWith(findings []agent.Finding, level agent.Level, text string) bool {
	for _, f := range findings {
		if f.Level == level && strings.Contains(f.Message, text) {
			return true
		}
	}
	return false
}

func TestInspectWarnsWhenThereIsNoRequirementsFile(t *testing.T) {
	a := codex.New()
	a.SystemDir = t.TempDir()

	findings := a.Inspect(nil)

	if !findingsWith(findings, agent.Warn, "requirements.toml") {
		t.Errorf("findings %v should warn that Codex is ungoverned until aw-sync runs", findings)
	}
}

func TestInspectReportsTheRequirementsRevision(t *testing.T) {
	a := codex.New()
	a.SystemDir = t.TempDir()
	content := "# Managed by aw-sync from policy revision 2026-09-22.7. Do not edit: the next sync overwrites this file.\n\nallowed_sandbox_modes = ['read-only']\n"
	if err := os.WriteFile(filepath.Join(a.SystemDir, codex.RequirementsFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := a.Inspect(nil)

	if !findingsWith(findings, agent.OK, "2026-09-22.7") {
		t.Errorf("findings %v should report the revision from the header", findings)
	}
}

func TestInspectFlagsARequirementsFileThatIsNotTOML(t *testing.T) {
	a := codex.New()
	a.SystemDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(a.SystemDir, codex.RequirementsFile), []byte("= not toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := a.Inspect(nil)

	if !findingsWith(findings, agent.Error, "requirements.toml") {
		t.Errorf("findings %v should flag the unparseable file", findings)
	}
}
