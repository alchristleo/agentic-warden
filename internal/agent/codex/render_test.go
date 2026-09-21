package codex_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/codex"
	"github.com/acme/agent-wrapper/internal/policy"
)

func codexRule(name string, repos []string, managed map[string]any) policy.Rule {
	return policy.Rule{
		Name:   name,
		Match:  policy.Match{Repos: repos},
		Agents: map[string]policy.AgentConfig{codex.Name: {Managed: managed}},
	}
}

// bundle is a platform user's bundle: a baseline for everyone, a stricter
// rule, a repo-scoped rule Codex cannot honour, and a Claude-only rule.
func bundle() *policy.Bundle {
	return &policy.Bundle{
		Version: "2026-09-22.1",
		Groups:  []string{"platform"},
		Rules: []policy.Rule{
			codexRule("baseline", nil, map[string]any{
				"allowed_sandbox_modes":     []any{"read-only", "workspace-write"},
				"allowed_approval_policies": []any{"on-request", "untrusted"},
				"mcp_servers":               map[string]any{"docs": map[string]any{"identity": map[string]any{"command": "codex-mcp"}}},
				"rules": map[string]any{"prefix_rules": []any{
					map[string]any{"pattern": []any{map[string]any{"token": "rm"}}, "decision": "forbidden", "justification": "Use git clean -fd instead."},
				}},
			}),
			codexRule("strict", nil, map[string]any{
				"allowed_sandbox_modes": []any{"read-only"},
				"rules": map[string]any{"prefix_rules": []any{
					map[string]any{"pattern": []any{map[string]any{"token": "git"}, map[string]any{"token": "push"}}, "decision": "prompt"},
				}},
			}),
			codexRule("payments", []string{"github.com/acme/payments*"}, map[string]any{
				"allowed_sandbox_modes": []any{"read-only"},
			}),
			{Name: "claude-only", Match: policy.Match{Repos: []string{"github.com/acme/x"}},
				Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
		},
	}
}

func TestRenderMatchesTheGoldenFile(t *testing.T) {
	rendering, err := codex.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(rendering.Files) != 1 {
		t.Fatalf("files = %d, want requirements.toml alone", len(rendering.Files))
	}
	f := rendering.Files[0]
	if f.Path != "requirements.toml" || f.Mode != 0o644 {
		t.Errorf("file = %s mode %o", f.Path, f.Mode)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "requirements.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Content) != string(want) {
		t.Errorf("rendered requirements.toml differs from testdata:\n%s\nwant:\n%s", f.Content, want)
	}
}

func TestRenderNotesTheRepoScopedRulesItDropped(t *testing.T) {
	rendering, err := codex.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}

	notes := strings.Join(rendering.Notes, "\n")
	if len(rendering.Notes) != 1 || !strings.Contains(notes, "1 repo-scoped rule") || !strings.Contains(notes, "payments") {
		t.Errorf("notes = %q; want one note naming the dropped Codex rule", rendering.Notes)
	}
	if strings.Contains(notes, "claude-only") {
		t.Error("the note counts a rule that does not configure Codex")
	}
}

func TestRenderWithNoCodexRulesWritesTheHeaderAlone(t *testing.T) {
	rendering, err := codex.New().Render(&policy.Bundle{Version: "v1", Rules: []policy.Rule{
		{Name: "claude", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if got := string(rendering.Files[0].Content); got != "# Managed by aw-sync from policy revision v1. Do not edit: the next sync overwrites this file.\n\n" {
		t.Errorf("content = %q", got)
	}
	if len(rendering.Notes) != 0 {
		t.Errorf("notes = %q; nothing was dropped", rendering.Notes)
	}
}

func TestRenderOfANilBundleIsEmptyAndUnversioned(t *testing.T) {
	rendering, err := codex.New().Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(rendering.Files[0].Content), "# Managed by aw-sync from policy revision unversioned.") {
		t.Errorf("content = %q", rendering.Files[0].Content)
	}
}

func TestRenderRejectsAnUnknownKey(t *testing.T) {
	_, err := codex.New().Render(&policy.Bundle{Rules: []policy.Rule{
		codexRule("bad", nil, map[string]any{"sandbox_mode": "read-only"}),
	}})

	if err == nil || !strings.Contains(err.Error(), `"sandbox_mode"`) {
		t.Errorf("err = %v; want the unknown key named", err)
	}
}

func TestRenderedDocumentReadsBackAsTOML(t *testing.T) {
	rendering, err := codex.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := codex.DecodeForTest(rendering.Files[0].Content, &back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if modes, _ := back["allowed_sandbox_modes"].([]any); len(modes) != 1 || modes[0] != "read-only" {
		t.Errorf("allowed_sandbox_modes = %v after the strict rule", modes)
	}
}
