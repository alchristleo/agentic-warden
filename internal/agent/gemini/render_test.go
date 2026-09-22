package gemini_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/gemini"
	"github.com/acme/agent-wrapper/internal/policy"
)

func geminiRule(name string, repos []string, managed map[string]any) policy.Rule {
	return policy.Rule{
		Name:   name,
		Match:  policy.Match{Repos: repos},
		Agents: map[string]policy.AgentConfig{gemini.Name: {Managed: managed}},
	}
}

// bundle is a platform user's bundle: a baseline for everyone, a stricter
// rule, a repo-scoped rule Gemini cannot honour, and a Claude-only rule.
// Priorities are float64 because that is how JSON numbers reach a renderer.
func bundle() *policy.Bundle {
	return &policy.Bundle{
		Version: "2026-09-22.1",
		Groups:  []string{"platform"},
		Rules: []policy.Rule{
			geminiRule("baseline", nil, map[string]any{
				"settings": map[string]any{
					"tools":      map[string]any{"exclude": []any{"run_shell_command"}},
					"mcp":        map[string]any{"allowed": []any{"docs"}},
					"mcpServers": map[string]any{"docs": map[string]any{"command": "gemini-mcp"}},
					"admin":      map[string]any{"secureModeEnabled": true},
				},
				"policies": []any{
					map[string]any{"toolName": "run_shell_command", "commandPrefix": "rm -rf", "decision": "deny", "priority": float64(100), "denyMessage": "Deletion is permanent"},
				},
			}),
			geminiRule("strict", nil, map[string]any{
				"settings": map[string]any{
					"tools":      map[string]any{"exclude": []any{"web_fetch"}},
					"mcp":        map[string]any{"allowed": []any{"jira"}},
					"mcpServers": map[string]any{"jira": map[string]any{"url": "https://jira/mcp"}},
					"admin":      map[string]any{"secureModeEnabled": false},
				},
				"policies": []any{
					map[string]any{"toolName": []any{"write_file", "replace"}, "decision": "ask_user", "priority": float64(50), "modes": []any{"default"}},
				},
			}),
			geminiRule("payments", []string{"github.com/acme/payments*"}, map[string]any{
				"policies": []any{map[string]any{"toolName": "*", "decision": "deny", "priority": float64(999)}},
			}),
			{Name: "claude-only", Match: policy.Match{Repos: []string{"github.com/acme/x"}},
				Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
		},
	}
}

// files indexes a rendering by path.
func files(t *testing.T, r agent.Rendering) map[string]agent.File {
	t.Helper()
	out := make(map[string]agent.File, len(r.Files))
	for _, f := range r.Files {
		out[f.Path] = f
	}
	return out
}

func TestRenderMatchesTheGoldenFiles(t *testing.T) {
	rendering, err := gemini.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(rendering.Files) != 2 {
		t.Fatalf("files = %d, want settings.json and the policy file", len(rendering.Files))
	}
	got := files(t, rendering)
	for path, golden := range map[string]string{
		gemini.SettingsFile: "settings.json",
		gemini.PoliciesFile: "50-agent-wrapper.toml",
	} {
		f, ok := got[path]
		if !ok || f.Mode != 0o644 {
			t.Errorf("%s: present %v mode %o", path, ok, f.Mode)
			continue
		}
		want, err := os.ReadFile(filepath.Join("testdata", golden))
		if err != nil {
			t.Fatal(err)
		}
		if string(f.Content) != string(want) {
			t.Errorf("%s differs from testdata/%s:\n%s\nwant:\n%s", path, golden, f.Content, want)
		}
	}
}

func TestRenderNotesTheRepoScopedRulesItDropped(t *testing.T) {
	rendering, err := gemini.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}

	notes := strings.Join(rendering.Notes, "\n")
	if len(rendering.Notes) != 1 || !strings.Contains(notes, "1 repo-scoped rule") || !strings.Contains(notes, "payments") {
		t.Errorf("notes = %q; want one note naming the dropped Gemini rule", rendering.Notes)
	}
	if strings.Contains(notes, "claude-only") {
		t.Error("the note counts a rule that does not configure Gemini")
	}
}

func TestRenderWithNoGeminiRulesWritesEmptyFiles(t *testing.T) {
	rendering, err := gemini.New().Render(&policy.Bundle{Version: "v1", Rules: []policy.Rule{
		{Name: "claude", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	got := files(t, rendering)
	if s := string(got[gemini.SettingsFile].Content); s != "{}\n" {
		t.Errorf("settings.json = %q; want an empty object so a stale file never lingers", s)
	}
	if p := string(got[gemini.PoliciesFile].Content); p != "# Managed by aw-sync from policy revision v1. Do not edit: the next sync overwrites this file.\n\n" {
		t.Errorf("policy file = %q; want the header alone", p)
	}
	if len(rendering.Notes) != 0 {
		t.Errorf("notes = %q; nothing was dropped", rendering.Notes)
	}
}

func TestRenderOfANilBundleIsEmptyAndUnversioned(t *testing.T) {
	rendering, err := gemini.New().Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := files(t, rendering)
	if !strings.HasPrefix(string(got[gemini.PoliciesFile].Content), "# Managed by aw-sync from policy revision unversioned.") {
		t.Errorf("policy file = %q", got[gemini.PoliciesFile].Content)
	}
	if string(got[gemini.SettingsFile].Content) != "{}\n" {
		t.Errorf("settings.json = %q", got[gemini.SettingsFile].Content)
	}
}

func TestRenderRejectsAnUnknownSettingsKey(t *testing.T) {
	_, err := gemini.New().Render(&policy.Bundle{Rules: []policy.Rule{
		geminiRule("bad", nil, map[string]any{"settings": map[string]any{"toolz": map[string]any{}}}),
	}})

	if err == nil || !strings.Contains(err.Error(), `"toolz"`) {
		t.Errorf("err = %v; want the unknown key named", err)
	}
}

func TestRenderRejectsAPolicyRuleWithoutAPriority(t *testing.T) {
	_, err := gemini.New().Render(&policy.Bundle{Rules: []policy.Rule{
		geminiRule("bad", nil, map[string]any{"policies": []any{map[string]any{"toolName": "*", "decision": "deny"}}}),
	}})

	if err == nil || !strings.Contains(err.Error(), "priority") {
		t.Errorf("err = %v; want priority named", err)
	}
}

func TestRenderedFilesReadBack(t *testing.T) {
	rendering, err := gemini.New().Render(bundle())
	if err != nil {
		t.Fatal(err)
	}
	got := files(t, rendering)

	var settings map[string]any
	if err := json.Unmarshal(got[gemini.SettingsFile].Content, &settings); err != nil {
		t.Fatalf("settings.json round trip: %v", err)
	}
	if admin, _ := settings["admin"].(map[string]any); admin["secureModeEnabled"] != false {
		t.Errorf("admin.secureModeEnabled = %v after the strict rule", admin["secureModeEnabled"])
	}

	var policies map[string]any
	if err := gemini.DecodeTOMLForTest(got[gemini.PoliciesFile].Content, &policies); err != nil {
		t.Fatalf("policy file round trip: %v", err)
	}
	rules, _ := policies["rule"].([]any)
	if len(rules) != 2 {
		t.Fatalf("rule = %v; want two [[rule]] tables", rules)
	}
	first, _ := rules[0].(map[string]any)
	if _, isInt := first["priority"].(int64); !isInt {
		t.Errorf("priority decoded as %T; Gemini requires a TOML integer, not a float", first["priority"])
	}
}
