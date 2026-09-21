package policy_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/policy"
)

func writeRuleSet(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

const authoredYAML = `
version: "2026-09-20.1"
rules:
  - name: baseline
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Read(./.env)
        env:
          CLAUDE_CODE_ENABLE_TELEMETRY: "1"
        forceEnv: true
  - name: payments
    match:
      repos:
        - github.com/acme/payments*
    agents:
      claude:
        managed:
          model: opus
`

func TestLoadRuleSetReadsAuthoredYAML(t *testing.T) {
	path := writeRuleSet(t, "policy.yaml", authoredYAML)

	set, err := policy.LoadRuleSet(path)
	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}

	if set.Version != "2026-09-20.1" {
		t.Errorf("Version = %q, want %q", set.Version, "2026-09-20.1")
	}
	if len(set.Rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(set.Rules))
	}
	claude := set.Rules[0].Agents["claude"]
	if claude.Env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("env = %v, want the telemetry variable", claude.Env)
	}
	if !claude.ForceEnv {
		t.Error("forceEnv did not survive the YAML round trip")
	}
	if got := set.Rules[1].Match.Repos; len(got) != 1 || got[0] != "github.com/acme/payments*" {
		t.Errorf("repos = %v, want the payments pattern", got)
	}
}

func TestLoadRuleSetReadsJSONToo(t *testing.T) {
	path := writeRuleSet(t, "policy.json", `{"version":"v1","rules":[{"name":"baseline"}]}`)

	set, err := policy.LoadRuleSet(path)
	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}

	if set.Version != "v1" || len(set.Rules) != 1 {
		t.Errorf("set = %+v, want the JSON document", set)
	}
}

func TestLoadRuleSetCompilesWhatItLoaded(t *testing.T) {
	path := writeRuleSet(t, "policy.yaml", authoredYAML)
	set, err := policy.LoadRuleSet(path)
	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}

	doc := set.Compile(policy.Subject{Repo: "github.com/acme/payments-api"})

	if got := doc.Agent("claude").Managed["model"]; got != "opus" {
		t.Errorf("model = %v, want the payments rule applied", got)
	}
}

func TestLoadRuleSetRejectsAnUnknownField(t *testing.T) {
	path := writeRuleSet(t, "policy.yaml", "version: v1\nrules:\n  - name: baseline\n    mach:\n      groups: [platform]\n")

	_, err := policy.LoadRuleSet(path)

	if err == nil {
		t.Fatal("LoadRuleSet() error = nil, want a typo in a policy key to be rejected rather than silently ignored")
	}
}

func TestLoadRuleSetRequiresAVersion(t *testing.T) {
	path := writeRuleSet(t, "policy.yaml", "rules:\n  - name: baseline\n")

	_, err := policy.LoadRuleSet(path)

	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("error = %v, want it to name the missing version", err)
	}
}

func TestLoadRuleSetRequiresEveryRuleToBeNamed(t *testing.T) {
	path := writeRuleSet(t, "policy.yaml", "version: v1\nrules:\n  - match:\n      groups: [platform]\n")

	_, err := policy.LoadRuleSet(path)

	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("error = %v, want it to report the unnamed rule", err)
	}
}

func TestLoadRuleSetRejectsDuplicateRuleNames(t *testing.T) {
	path := writeRuleSet(t, "policy.yaml", "version: v1\nrules:\n  - name: baseline\n  - name: baseline\n")

	_, err := policy.LoadRuleSet(path)

	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Errorf("error = %v, want the duplicated rule name reported", err)
	}
}

func TestLoadRuleSetNamesTheFileItCouldNotRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	_, err := policy.LoadRuleSet(missing)

	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %v, want it to name %q", err, missing)
	}
}

func TestValidateAcceptsARuleSetWithNoRules(t *testing.T) {
	set := policy.RuleSet{Version: "v1"}

	if err := set.Validate(); err != nil {
		t.Errorf("Validate() = %v, want an empty policy to be valid", err)
	}
}

// TestTheShippedExamplePolicyIsValid keeps the example in the README honest.
// An example that no longer loads is worse than no example.
func TestTheShippedExamplePolicyIsValid(t *testing.T) {
	set, err := policy.LoadRuleSet(filepath.Join("..", "..", "examples", "org-policy.yaml"))
	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}

	doc := set.Compile(policy.Subject{Groups: []string{"platform"}, Repo: "github.com/acme/payments-api"})

	if len(doc.AppliedRules) != 3 {
		t.Errorf("AppliedRules = %v, want all three example rules to apply", doc.AppliedRules)
	}
	if got := doc.Agent("claude").Managed["model"]; got != "opus" {
		t.Errorf("model = %v, want the example's group rule to apply", got)
	}
}

func TestValidateRunsTheManagedValidatorPerAgent(t *testing.T) {
	set := policy.RuleSet{Version: "v1", Rules: []policy.Rule{
		{Name: "baseline", Agents: map[string]policy.AgentConfig{
			"claude": {Managed: map[string]any{"model": "opus"}},
		}},
		{Name: "payments", Agents: map[string]policy.AgentConfig{
			"claude": {Managed: map[string]any{"permissions": "broken"}},
		}},
	}}
	validator := func(agentName string, managed map[string]any) error {
		if _, broken := managed["permissions"].(string); broken {
			return errors.New("permissions must be an object")
		}
		return nil
	}

	err := set.Validate(validator)

	if err == nil {
		t.Fatal("Validate() = nil, want the validator's error")
	}
	for _, want := range []string{"payments", "claude", "permissions must be an object"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s so the author can find the rule", err, want)
		}
	}
}

func TestLoadRuleSetPassesValidatorsThrough(t *testing.T) {
	path := writeRuleSet(t, "policy.yaml", authoredYAML)
	calls := 0
	validator := func(string, map[string]any) error { calls++; return nil }

	if _, err := policy.LoadRuleSet(path, validator); err != nil {
		t.Fatalf("LoadRuleSet() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("validator ran %d times, want once per agent config (2)", calls)
	}
}
