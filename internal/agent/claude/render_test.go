package claude_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/policy"
)

func sampleBundle() *policy.Bundle {
	return &policy.Bundle{
		Version: "2026-09-21.1",
		User:    "alice@acme.com",
		Groups:  []string{"platform"},
		Rules: []policy.Rule{
			{Name: "baseline", Agents: map[string]policy.AgentConfig{
				"claude": {Managed: map[string]any{"model": "opus"}},
			}},
			{Name: "codex-only", Agents: map[string]policy.AgentConfig{
				"codex": {Managed: map[string]any{"allowed_models": []any{"gpt-5"}}},
			}},
			{Name: "payments", Match: policy.Match{Repos: []string{"github.com/acme/payments*"}},
				Agents: map[string]policy.AgentConfig{
					"claude": {Managed: map[string]any{"permissions": map[string]any{"deny": []any{"Bash(curl *)"}}}},
					"codex":  {Managed: map[string]any{"allowed_models": []any{"gpt-5"}}},
				}},
		},
	}
}

func fileNamed(t *testing.T, r agent.Rendering, path string) agent.File {
	t.Helper()
	for _, f := range r.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no rendered file %q; got %d files", path, len(r.Files))
	return agent.File{}
}

func TestRenderKeepsOnlyRulesThatMentionClaudeWithTheirMatchers(t *testing.T) {
	r, err := (&claude.Adapter{GOOS: "linux"}).Render(sampleBundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Notes) != 0 {
		t.Errorf("notes = %v, want none: Claude enforces every rule per launch", r.Notes)
	}
	var out policy.Bundle
	if err := json.Unmarshal(fileNamed(t, r, claude.BundleFile).Content, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != "2026-09-21.1" || out.User != "alice@acme.com" || !reflect.DeepEqual(out.Groups, []string{"platform"}) {
		t.Errorf("header not carried through: %+v", out)
	}
	if len(out.Rules) != 2 || out.Rules[0].Name != "baseline" || out.Rules[1].Name != "payments" {
		t.Fatalf("rules = %+v, want baseline and payments in order", out.Rules)
	}
	if !reflect.DeepEqual(out.Rules[1].Match.Repos, []string{"github.com/acme/payments*"}) {
		t.Errorf("repo matcher lost: %+v", out.Rules[1].Match)
	}
	if _, leaked := out.Rules[1].Agents["codex"]; leaked {
		t.Error("the Claude bundle carries another agent's configuration")
	}
	if _, kept := out.Rules[1].Agents["claude"]; !kept {
		t.Error("the Claude configuration was dropped")
	}
}

func TestRenderOfANilOrEmptyBundleIsAnEmptyList(t *testing.T) {
	for _, b := range []*policy.Bundle{nil, {}} {
		r, err := (&claude.Adapter{GOOS: "linux"}).Render(b)
		if err != nil {
			t.Fatal(err)
		}
		content := string(fileNamed(t, r, claude.BundleFile).Content)
		var out map[string]any
		if err := json.Unmarshal([]byte(content), &out); err != nil {
			t.Fatal(err)
		}
		if rules, ok := out["rules"].([]any); !ok || len(rules) != 0 {
			t.Errorf("rules = %v, want []", out["rules"])
		}
		if groups, ok := out["groups"].([]any); !ok || len(groups) != 0 {
			t.Errorf("groups = %v, want []", out["groups"])
		}
	}
}

func TestRenderedDropInEqualsTheInstallTemplate(t *testing.T) {
	cases := []struct{ goos, template string }{
		{"linux", "unix"},
		{"darwin", "unix"},
		{"windows", "windows"},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			r, err := (&claude.Adapter{GOOS: tc.goos}).Render(sampleBundle())
			if err != nil {
				t.Fatal(err)
			}
			got := fileNamed(t, r, claude.DropInFile).Content
			want, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "managed-settings", tc.template, "managed-settings.d", "50-agent-wrapper.json"))
			if err != nil {
				t.Fatal(err)
			}
			var gotDoc, wantDoc map[string]any
			if err := json.Unmarshal(got, &gotDoc); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want, &wantDoc); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotDoc, wantDoc) {
				t.Errorf("rendered drop-in differs from deploy/managed-settings/%s:\n%s\nwant:\n%s", tc.template, got, want)
			}
			if err := schema.Validate(gotDoc); err != nil {
				t.Errorf("drop-in fails the schema: %v", err)
			}
		})
	}
}

func TestRenderRejectsManagedSettingsTheSchemaRejects(t *testing.T) {
	b := &policy.Bundle{Version: "v", Rules: []policy.Rule{
		{Name: "bad", Agents: map[string]policy.AgentConfig{
			"claude": {Managed: map[string]any{"permissions": "nope"}},
		}},
	}}
	r, err := (&claude.Adapter{GOOS: "linux"}).Render(b)
	if err == nil {
		t.Fatal("want an error: a bundle aw-policy would refuse must not be written")
	}
	if len(r.Files) != 0 {
		t.Errorf("files returned alongside an error: %d", len(r.Files))
	}
}

func TestRenderedFilesAreReadableByEveryone(t *testing.T) {
	r, err := (&claude.Adapter{GOOS: "linux"}).Render(sampleBundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Files) != 2 {
		t.Fatalf("got %d files, want the bundle and the drop-in", len(r.Files))
	}
	for _, f := range r.Files {
		if f.Mode != 0o644 {
			t.Errorf("%s mode = %o, want 0644: the developer's helper reads it", f.Path, f.Mode)
		}
		if filepath.IsAbs(f.Path) {
			t.Errorf("%s is absolute; paths are relative to the system directory", f.Path)
		}
	}
}
