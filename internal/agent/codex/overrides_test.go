package codex

import (
	"strings"
	"testing"
)

func TestOverridesFlattenSortAndEncodeAsTOML(t *testing.T) {
	got, err := overrides(map[string]any{
		"sandbox_mode":    "read-only",
		"approval_policy": "untrusted",
		"model_reasoning": map[string]any{"effort": "high"},
		"mcp_servers":     map[string]any{"docs": map[string]any{"command": "codex-mcp", "args": []any{"--port", float64(8080)}}},
		"features":        map[string]any{"web_search": true, "ratio": 0.5},
		"note":            `say "hi"`,
		"url":             "https://a?b=1&c=<d>",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		`approval_policy="untrusted"`,
		`features.ratio=0.5`,
		`features.web_search=true`,
		`mcp_servers.docs.args=["--port", 8080]`,
		`mcp_servers.docs.command="codex-mcp"`,
		`model_reasoning.effort="high"`,
		`note="say \"hi\""`,
		`sandbox_mode="read-only"`,
		`url="https://a?b=1&c=<d>"`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("overrides =\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOverridesKeepTablesInsideArraysInline(t *testing.T) {
	got, err := overrides(map[string]any{"servers": []any{map[string]any{"name": "a", "port": float64(1)}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != `servers=[{name = "a", port = 1}]` {
		t.Errorf("overrides = %q", got)
	}
}

func TestOverridesRejectNull(t *testing.T) {
	_, err := overrides(map[string]any{"a": map[string]any{"b": nil}})
	if err == nil || !strings.Contains(err.Error(), "a.b") {
		t.Errorf("err = %v; want the null's path", err)
	}
}

func TestOverridesOfNothingAreNothing(t *testing.T) {
	if got, err := overrides(nil); err != nil || len(got) != 0 {
		t.Errorf("overrides(nil) = %v, %v", got, err)
	}
}
