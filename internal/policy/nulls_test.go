package policy_test

import (
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

func TestFirstNull(t *testing.T) {
	cases := []struct {
		name string
		v    any
		path string
		want string
	}{
		{"nil at top", nil, "launch", "launch"},
		{"nested map", map[string]any{"sandbox": map[string]any{"mode": nil}}, "launch", "launch.sandbox.mode"},
		{"inside a list", map[string]any{"items": []any{"a", nil}}, "launch", "launch.items[1]"},
		{"none", map[string]any{"a": 1, "b": "two"}, "launch", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := policy.FirstNull(c.v, c.path); got != c.want {
				t.Errorf("FirstNull(%v, %q) = %q, want %q", c.v, c.path, got, c.want)
			}
		})
	}
}

func TestIsBareKey(t *testing.T) {
	valid := []string{"a", "my-server_2", "A9"}
	for _, s := range valid {
		if !policy.IsBareKey(s) {
			t.Errorf("IsBareKey(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "my server", "a.b", "x=y", "é"}
	for _, s := range invalid {
		if policy.IsBareKey(s) {
			t.Errorf("IsBareKey(%q) = true, want false", s)
		}
	}
}

func TestFirstBadKey(t *testing.T) {
	cases := []struct {
		name     string
		v        any
		path     string
		wantPath string
		wantKey  string
	}{
		{"clean keys", map[string]any{"sandbox_mode": "read-only", "a-b": 1}, "launch", "", ""},
		{"a space", map[string]any{"mcp_servers": map[string]any{"my server": map[string]any{"command": "x"}}},
			"launch", "launch.mcp_servers.my server", "my server"},
		{"a dot inside a key", map[string]any{"a.b": 1}, "launch", "launch.a.b", "a.b"},
		{"an equals sign", map[string]any{"x=y": 1}, "launch", "launch.x=y", "x=y"},
		{"inside a list", map[string]any{"items": []any{map[string]any{"bad key": 1}}},
			"launch", "launch.items[0].bad key", "bad key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotPath, gotKey := policy.FirstBadKey(c.v, c.path)
			if gotPath != c.wantPath || gotKey != c.wantKey {
				t.Errorf("FirstBadKey(%v, %q) = (%q, %q), want (%q, %q)", c.v, c.path, gotPath, gotKey, c.wantPath, c.wantKey)
			}
		})
	}
}
