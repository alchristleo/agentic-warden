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
