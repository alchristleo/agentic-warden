package policy

import (
	"github.com/acme/agent-wrapper/internal/glob"
	"github.com/acme/agent-wrapper/internal/merge"
)

// Subject is who a policy is being compiled for. It is the whole of what
// targeting can see, so a rule can never depend on anything the client did not
// present and the server did not verify.
type Subject struct {
	// Groups are the subject's group memberships, as mapped from identity
	// claims.
	Groups []string
	// Repo identifies the repository the session runs in, empty outside one.
	Repo string
}

// Match narrows which subjects a rule applies to. An empty field imposes no
// constraint, so the zero Match applies to everyone. Every field that is set
// must match.
type Match struct {
	// Groups matches when the subject belongs to any one of them. Group names
	// compare exactly: they come from an identity provider, and a wildcard
	// there would silently widen a rule's audience.
	Groups []string `json:"groups,omitempty"`
	// Repos matches when the subject's repository matches any pattern. A
	// subject outside a repository matches no repo-scoped rule.
	Repos []string `json:"repos,omitempty"`
}

// Matches reports whether m applies to s.
func (m Match) Matches(s Subject) bool {
	if len(m.Groups) > 0 && !anyIn(m.Groups, s.Groups) {
		return false
	}
	if len(m.Repos) > 0 {
		if s.Repo == "" {
			return false
		}
		if _, ok := glob.MatchAny(m.Repos, s.Repo); !ok {
			return false
		}
	}
	return true
}

func anyIn(wanted, have []string) bool {
	for _, w := range wanted {
		for _, h := range have {
			if w == h {
				return true
			}
		}
	}
	return false
}

// Rule is one targeted piece of an organization's policy.
type Rule struct {
	// Name identifies the rule in diagnostics and audit output.
	Name  string `json:"name"`
	Match Match  `json:"match,omitempty"`
	// Agents is the configuration this rule contributes, per agent.
	Agents map[string]AgentConfig `json:"agents,omitempty"`
}

// RuleSet is a complete authored policy.
type RuleSet struct {
	// Version identifies this revision. Clients cache on it.
	Version string `json:"version,omitempty"`
	// Groups maps a user to the groups they belong to. It is authored with
	// the rules and versioned with them, so a membership change is a policy
	// revision like any other. An identity-provider sync can replace this
	// map later without changing what a client receives.
	Groups map[string][]string `json:"groups,omitempty"`
	// Rules apply in order. A later rule overrides an earlier one per key, so
	// the author controls precedence by ordering rather than by a priority
	// field that has to be kept consistent.
	Rules []Rule `json:"rules,omitempty"`
}

// AgentMergeRules says how one agent's managed settings combine across the
// rules that apply to a subject. Claude's permission lists union so an
// organization allowlist and a team allowlist coexist instead of one
// silently erasing the other. Codex's requirements follow Codex's own
// layering: scalars and allowlists replace (a union would widen an
// allowlist), tables such as mcp_servers merge by key, and only
// rules.prefix_rules accumulate, since each entry is its own restriction.
func AgentMergeRules(agentName string) merge.Rules {
	switch agentName {
	case "claude":
		return merge.Rules{UnionArrays: []string{
			"permissions.allow",
			"permissions.deny",
			"permissions.ask",
			"permissions.additionalDirectories",
		}}
	case "codex":
		return merge.Rules{UnionArrays: []string{"rules.prefix_rules"}}
	default:
		return merge.Rules{}
	}
}

// Compile resolves the rule set for one subject into the document a client
// applies. It is the server-side slice followed by the client-side compile,
// in one call, for callers that know the whole subject at once. It never
// mutates the rule set, so the same RuleSet can serve concurrent requests.
func (rs *RuleSet) Compile(s Subject) *Document {
	return rs.Slice(s.Groups).Compile(s.Repo)
}

// mergeAgent layers one rule's contribution for a single agent on top of what
// earlier rules produced.
func (d *Document) mergeAgent(agentName string, config AgentConfig) {
	if d.Agents == nil {
		d.Agents = make(map[string]AgentConfig)
	}
	current := d.Agents[agentName]

	if config.Managed != nil {
		merged, _ := merge.JSON(cloneMap(current.Managed), config.Managed,
			AgentMergeRules(agentName)).(map[string]any)
		current.Managed = merged
	}
	if len(config.Env) > 0 {
		if current.Env == nil {
			current.Env = make(map[string]string, len(config.Env))
		}
		for k, v := range config.Env {
			current.Env[k] = v
		}
	}
	// Forcing is one-way: once any matching rule requires the organization's
	// environment to win, a later rule cannot quietly relax it.
	current.ForceEnv = current.ForceEnv || config.ForceEnv

	d.Agents[agentName] = current
}

// cloneMap returns a shallow copy, or an empty map for nil. merge.JSON reads
// its inputs and builds new maps as it goes, but the top-level map it is
// handed for an unmerged key is carried through by reference, so the copy
// keeps a rule's own document from being reachable from the result.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
