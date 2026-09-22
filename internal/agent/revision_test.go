package agent_test

import (
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
)

func TestRevisionFromHeader(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "header present",
			content: "# Managed by aw-sync from policy revision 2026-09-22.7. Do not edit: the next sync overwrites this file.\n\nallowed_sandbox_modes = ['read-only']\n",
			want:    "2026-09-22.7",
		},
		{
			name:    "header absent",
			content: "allowed_sandbox_modes = ['read-only']\n",
			want:    "",
		},
		{
			name:    "different prefix",
			content: "# Written by someone else from policy revision 2026-09-22.7\n",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agent.RevisionFromHeader([]byte(tc.content)); got != tc.want {
				t.Errorf("RevisionFromHeader(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}
