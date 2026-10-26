package agent_test

import (
	"testing"

	"github.com/alchristleo/agentic-warden/internal/agent"
	"github.com/alchristleo/agentic-warden/internal/policy"
)

func TestDroppedRepoRules(t *testing.T) {
	repoRule := func(name, agentName string) policy.Rule {
		return policy.Rule{Name: name,
			Match:  policy.Match{Repos: []string{"github.com/acme/x"}},
			Agents: map[string]policy.AgentConfig{agentName: {}}}
	}
	tests := []struct {
		name   string
		bundle *policy.Bundle
		want   []string
	}{
		{"nil bundle", nil, nil},
		{"no rules", &policy.Bundle{}, nil},
		{"unscoped rule", &policy.Bundle{Rules: []policy.Rule{
			{Name: "all", Agents: map[string]policy.AgentConfig{"codex": {}}}}}, nil},
		{"two repo-scoped rules", &policy.Bundle{Rules: []policy.Rule{
			repoRule("payments", "codex"), repoRule("billing", "codex")}},
			[]string{"codex: 2 repo-scoped rule(s) not enforceable in requirements.toml: payments, billing"}},
		{"other agent's rule ignored", &policy.Bundle{Rules: []policy.Rule{
			repoRule("payments", "claude"), repoRule("billing", "codex")}},
			[]string{"codex: 1 repo-scoped rule(s) not enforceable in requirements.toml: billing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agent.DroppedRepoRules(tt.bundle, "codex", "requirements.toml")
			if len(got) != len(tt.want) || (tt.want != nil && got[0] != tt.want[0]) {
				t.Errorf("DroppedRepoRules = %q, want %q", got, tt.want)
			}
		})
	}
}
