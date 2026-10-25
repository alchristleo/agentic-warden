package agy_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/agent/agy"
	"github.com/alchristleo/agentic-warden/internal/policy"
)

func agyRule(name string, repos []string, managed map[string]any) policy.Rule {
	return policy.Rule{
		Name:   name,
		Match:  policy.Match{Repos: repos},
		Agents: map[string]policy.AgentConfig{agy.Name: {Managed: managed}},
	}
}

func goldenDoc() map[string]any {
	return map[string]any{
		"gateway": map[string]any{
			"enabled":       true,
			"url":           "https://gateway.example.com",
			"wireProtocol":  "genai",
			"customHeaders": map[string]any{"X-Cost-Center": "platform"},
		},
		"caCertPath": "/etc/ssl/certs/example-ca.pem",
		"models": map[string]any{
			"defaultModelId": "gemini-3.8-flash",
			"list": []any{
				map[string]any{"modelId": "gemini-3.8-flash"},
				map[string]any{"modelId": "gemini-3.8-pro"},
			},
		},
	}
}

func TestRenderMatchesTheGoldenFiles(t *testing.T) {
	r, err := agy.New().Render(&policy.Bundle{Version: "v1", Rules: []policy.Rule{agyRule("gw", nil, goldenDoc())}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Files) != 1 || r.Files[0].Path != "admin_settings.json" || r.Files[0].Mode != 0o644 {
		t.Fatalf("files = %+v", r.Files)
	}
	want, err := os.ReadFile("testdata/admin_settings.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture is hand-ordered, the encoder sorts keys: compare as JSON.
	var got, wantDoc any
	if err := json.Unmarshal(r.Files[0].Content, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantDoc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantDoc) {
		t.Errorf("content =\n%s\nwant\n%s", r.Files[0].Content, want)
	}
	if c := string(r.Files[0].Content); !strings.HasPrefix(c, "{\n  \"") || !strings.HasSuffix(c, "}\n") {
		t.Errorf("content is not indented JSON with a trailing newline: %q", c)
	}
}

func TestRenderWithNoAgyRulesWritesEmpty(t *testing.T) {
	b := &policy.Bundle{Rules: []policy.Rule{
		{Name: "claude-only", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
	}}
	r, err := agy.New().Render(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Files) != 1 || string(r.Files[0].Content) != "{}\n" {
		t.Errorf("files = %+v, want one {} file", r.Files)
	}
	if len(r.Notes) != 0 {
		t.Errorf("notes = %v", r.Notes)
	}
}

func TestRenderNotesTheRepoScopedRulesItDropped(t *testing.T) {
	b := &policy.Bundle{Rules: []policy.Rule{
		agyRule("gw", nil, goldenDoc()),
		agyRule("payments", []string{"github.com/acme/payments*"}, map[string]any{"caCertPath": "/x.pem"}),
		{Name: "claude-scoped", Match: policy.Match{Repos: []string{"github.com/acme/x"}},
			Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}}},
	}}
	r, err := agy.New().Render(b)
	if err != nil {
		t.Fatal(err)
	}
	want := "agy: 1 repo-scoped rule(s) not enforceable in admin_settings.json: payments"
	if len(r.Notes) != 1 || r.Notes[0] != want {
		t.Errorf("notes = %v, want [%q]", r.Notes, want)
	}
	if strings.Contains(string(r.Files[0].Content), "/x.pem") {
		t.Error("repo-scoped rule leaked into the file")
	}
}

func TestRenderRejectsAnInvalidDocument(t *testing.T) {
	b := &policy.Bundle{Rules: []policy.Rule{agyRule("bad", nil, map[string]any{"bogus": 1})}}
	if _, err := agy.New().Render(b); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("err = %v, want one naming bogus", err)
	}
}
