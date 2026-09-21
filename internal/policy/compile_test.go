package policy_test

import (
	"reflect"
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

func claudeManaged(t *testing.T, doc *policy.Document) map[string]any {
	t.Helper()
	return doc.Agent("claude").Managed
}

func TestCompileWithNoRulesProducesAnEmptyDocument(t *testing.T) {
	var set policy.RuleSet

	doc := set.Compile(policy.Subject{})

	if len(doc.Agents) != 0 {
		t.Errorf("Agents = %v, want none", doc.Agents)
	}
}

func TestCompileAppliesARuleThatTargetsNothingInParticular(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{{
		Name:   "baseline",
		Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "sonnet"}}},
	}}}

	doc := set.Compile(policy.Subject{})

	if got := claudeManaged(t, doc)["model"]; got != "sonnet" {
		t.Errorf("model = %v, want %q", got, "sonnet")
	}
}

func TestCompileAppliesAGroupRuleToAMember(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{{
		Name:   "platform team",
		Match:  policy.Match{Groups: []string{"platform"}},
		Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}},
	}}}

	doc := set.Compile(policy.Subject{Groups: []string{"platform", "oncall"}})

	if got := claudeManaged(t, doc)["model"]; got != "opus" {
		t.Errorf("model = %v, want the group rule to apply", got)
	}
}

func TestCompileSkipsAGroupRuleForSomeoneOutsideIt(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{{
		Name:   "platform team",
		Match:  policy.Match{Groups: []string{"platform"}},
		Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}},
	}}}

	doc := set.Compile(policy.Subject{Groups: []string{"design"}})

	if len(doc.Agents) != 0 {
		t.Errorf("Agents = %v, want the rule skipped", doc.Agents)
	}
}

func TestCompileMatchesARepositoryPattern(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{{
		Name:   "regulated repos",
		Match:  policy.Match{Repos: []string{"github.com/acme/payments*"}},
		Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}},
	}}}

	doc := set.Compile(policy.Subject{Repo: "github.com/acme/payments-api"})

	if got := claudeManaged(t, doc)["model"]; got != "opus" {
		t.Errorf("model = %v, want the repo rule to apply", got)
	}
}

func TestCompileSkipsARepoRuleWhenTheSubjectHasNoRepository(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{{
		Name:   "regulated repos",
		Match:  policy.Match{Repos: []string{"github.com/acme/payments*"}},
		Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}},
	}}}

	doc := set.Compile(policy.Subject{})

	if len(doc.Agents) != 0 {
		t.Errorf("Agents = %v, want a repo-scoped rule skipped outside a repository", doc.Agents)
	}
}

func TestCompileRequiresEveryStatedCriterionToMatch(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{{
		Name: "platform team in payments",
		Match: policy.Match{
			Groups: []string{"platform"},
			Repos:  []string{"github.com/acme/payments*"},
		},
		Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}},
	}}}

	doc := set.Compile(policy.Subject{Groups: []string{"platform"}, Repo: "github.com/acme/web"})

	if len(doc.Agents) != 0 {
		t.Errorf("Agents = %v, want the rule skipped when only the group matches", doc.Agents)
	}
}

func TestCompileLetsALaterRuleOverrideAnEarlierOne(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{
		{
			Name:   "baseline",
			Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "sonnet", "cleanupPeriodDays": float64(30)}}},
		},
		{
			Name:   "platform override",
			Match:  policy.Match{Groups: []string{"platform"}},
			Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "opus"}}},
		},
	}}

	doc := set.Compile(policy.Subject{Groups: []string{"platform"}})

	managed := claudeManaged(t, doc)
	if managed["model"] != "opus" {
		t.Errorf("model = %v, want the later rule to win", managed["model"])
	}
	if managed["cleanupPeriodDays"] != float64(30) {
		t.Errorf("cleanupPeriodDays = %v, want the baseline value to survive", managed["cleanupPeriodDays"])
	}
}

func TestCompileUnionsPermissionListsAcrossRules(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{
		{
			Name: "baseline",
			Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{
				"permissions": map[string]any{"deny": []any{"Read(./.env)"}},
			}}},
		},
		{
			Name:  "payments",
			Match: policy.Match{Repos: []string{"github.com/acme/payments*"}},
			Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{
				"permissions": map[string]any{"deny": []any{"Bash(curl *)"}},
			}}},
		},
	}}

	doc := set.Compile(policy.Subject{Repo: "github.com/acme/payments-api"})

	permissions, _ := claudeManaged(t, doc)["permissions"].(map[string]any)
	deny, _ := permissions["deny"].([]any)
	want := []any{"Read(./.env)", "Bash(curl *)"}
	if !reflect.DeepEqual(deny, want) {
		t.Errorf("permissions.deny = %v, want %v", deny, want)
	}
}

func TestCompileMergesEnvironmentWithTheLaterRuleWinning(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{
		{Name: "a", Agents: map[string]policy.AgentConfig{"claude": {Env: map[string]string{
			"ANTHROPIC_BASE_URL": "https://gateway.acme.com", "OTEL_LOG_USER_PROMPTS": "0",
		}}}},
		{Name: "b", Agents: map[string]policy.AgentConfig{"claude": {Env: map[string]string{
			"OTEL_LOG_USER_PROMPTS": "1",
		}}}},
	}}

	doc := set.Compile(policy.Subject{})

	env := doc.Agent("claude").Env
	if env["ANTHROPIC_BASE_URL"] != "https://gateway.acme.com" {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want it carried from the first rule", env["ANTHROPIC_BASE_URL"])
	}
	if env["OTEL_LOG_USER_PROMPTS"] != "1" {
		t.Errorf("OTEL_LOG_USER_PROMPTS = %q, want the later rule to win", env["OTEL_LOG_USER_PROMPTS"])
	}
}

func TestCompileTurnsForceEnvOnWhenAnyMatchingRuleAsksForIt(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{
		{Name: "a", Agents: map[string]policy.AgentConfig{"claude": {Env: map[string]string{"A": "1"}}}},
		{Name: "b", Agents: map[string]policy.AgentConfig{"claude": {ForceEnv: true}}},
	}}

	doc := set.Compile(policy.Subject{})

	if !doc.Agent("claude").ForceEnv {
		t.Error("ForceEnv = false, want it on once any matching rule requires it")
	}
}

func TestCompileKeepsAgentsSeparate(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{{
		Name: "baseline",
		Agents: map[string]policy.AgentConfig{
			"claude": {Managed: map[string]any{"model": "opus"}},
			"codex":  {Managed: map[string]any{"model": "gpt-5"}},
		},
	}}}

	doc := set.Compile(policy.Subject{})

	if got := doc.Agent("claude").Managed["model"]; got != "opus" {
		t.Errorf("claude model = %v, want %q", got, "opus")
	}
	if got := doc.Agent("codex").Managed["model"]; got != "gpt-5" {
		t.Errorf("codex model = %v, want %q", got, "gpt-5")
	}
}

func TestCompileCarriesTheRuleSetVersionSoAClientCanCacheOnIt(t *testing.T) {
	set := policy.RuleSet{Version: "2026-09-20.3"}

	if got := set.Compile(policy.Subject{}).Version; got != "2026-09-20.3" {
		t.Errorf("Version = %q, want %q", got, "2026-09-20.3")
	}
}

func TestCompileRecordsWhichRulesApplied(t *testing.T) {
	set := policy.RuleSet{Rules: []policy.Rule{
		{Name: "baseline", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{"model": "sonnet"}}}},
		{Name: "design only", Match: policy.Match{Groups: []string{"design"}}},
		{Name: "platform", Match: policy.Match{Groups: []string{"platform"}}, Agents: map[string]policy.AgentConfig{"claude": {}}},
	}}

	doc := set.Compile(policy.Subject{Groups: []string{"platform"}})

	want := []string{"baseline", "platform"}
	if !reflect.DeepEqual(doc.AppliedRules, want) {
		t.Errorf("AppliedRules = %v, want %v", doc.AppliedRules, want)
	}
}

func TestCompileDoesNotMutateTheRuleSet(t *testing.T) {
	managed := map[string]any{"permissions": map[string]any{"deny": []any{"Read(./.env)"}}}
	set := policy.RuleSet{Rules: []policy.Rule{
		{Name: "a", Agents: map[string]policy.AgentConfig{"claude": {Managed: managed}}},
		{Name: "b", Agents: map[string]policy.AgentConfig{"claude": {Managed: map[string]any{
			"permissions": map[string]any{"deny": []any{"Bash(curl *)"}},
		}}}},
	}}

	set.Compile(policy.Subject{})
	set.Compile(policy.Subject{})

	permissions, _ := managed["permissions"].(map[string]any)
	deny, _ := permissions["deny"].([]any)
	if len(deny) != 1 {
		t.Errorf("the rule set's own deny list grew to %v; Compile must not mutate its input", deny)
	}
}

func TestCompileOnANilRuleSetIsSafe(t *testing.T) {
	var set *policy.RuleSet

	doc := set.Compile(policy.Subject{})

	if doc == nil || len(doc.Agents) != 0 {
		t.Errorf("Compile() on a nil rule set = %v, want an empty document", doc)
	}
}

func TestCodexRulesMergeWithCodexSemantics(t *testing.T) {
	// Codex's own layering replaces scalars and lists and merges tables by
	// key; a union on an allowlist would widen it. Only prefix_rules
	// accumulate, since each is a separate restriction.
	rs := &policy.RuleSet{Rules: []policy.Rule{
		{Name: "baseline", Agents: map[string]policy.AgentConfig{"codex": {Managed: map[string]any{
			"allowed_sandbox_modes": []any{"read-only", "workspace-write"},
			"default_permissions":   ":workspace",
			"mcp_servers":           map[string]any{"docs": map[string]any{"identity": map[string]any{"command": "codex-mcp"}}},
			"rules":                 map[string]any{"prefix_rules": []any{map[string]any{"pattern": []any{"rm"}, "decision": "forbidden"}}},
		}}}},
		{Name: "strict", Agents: map[string]policy.AgentConfig{"codex": {Managed: map[string]any{
			"allowed_sandbox_modes": []any{"read-only"},
			"default_permissions":   ":read-only",
			"mcp_servers":           map[string]any{"jira": map[string]any{"identity": map[string]any{"url": "https://jira/mcp"}}},
			"rules":                 map[string]any{"prefix_rules": []any{map[string]any{"pattern": []any{"git", "push"}, "decision": "prompt"}}},
		}}}},
	}}

	managed := rs.Compile(policy.Subject{}).Agent("codex").Managed

	if modes, _ := managed["allowed_sandbox_modes"].([]any); len(modes) != 1 || modes[0] != "read-only" {
		t.Errorf("allowed_sandbox_modes = %v; a later rule's allowlist must replace, not widen", modes)
	}
	if managed["default_permissions"] != ":read-only" {
		t.Errorf("default_permissions = %v; scalars replace", managed["default_permissions"])
	}
	servers, _ := managed["mcp_servers"].(map[string]any)
	if _, docs := servers["docs"]; !docs {
		t.Error("mcp_servers lost docs; tables merge by name")
	}
	if _, jira := servers["jira"]; !jira {
		t.Error("mcp_servers lost jira; tables merge by name")
	}
	rules, _ := managed["rules"].(map[string]any)
	if prefix, _ := rules["prefix_rules"].([]any); len(prefix) != 2 {
		t.Errorf("prefix_rules = %v; want both rules appended in order", prefix)
	}
}
