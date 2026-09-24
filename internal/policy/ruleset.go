package policy

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// LoadRuleSet reads an authored policy from disk. YAML and JSON are both
// accepted: YAML is converted to JSON and decoded through the same struct
// tags, so the two formats can never drift.
//
// Unknown fields are rejected. A misspelled key in a security policy that
// silently does nothing is the failure mode worth spending an error on.
func LoadRuleSet(path string, validators ...ManagedValidator) (*RuleSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("policy: reading %s: %w", path, err)
	}
	var set RuleSet
	if err := yaml.UnmarshalStrict(raw, &set); err != nil {
		return nil, fmt.Errorf("policy: parsing %s: %w", path, err)
	}
	if err := set.Validate(validators...); err != nil {
		return nil, fmt.Errorf("policy: %s: %w", path, err)
	}
	return &set, nil
}

// ManagedValidator checks one rule's managed settings for one agent, in that
// agent's own schema. The policy package knows nothing about any agent's
// schema; callers that do supply one of these.
type ManagedValidator func(agentName string, managed map[string]any) error

// Validate reports whether the rule set is well formed, and runs each
// validator over every agent configuration a rule carries. A policy with no
// rules is valid: an organization that has authored nothing yet still has a
// policy, and it is the empty one.
func (rs *RuleSet) Validate(validators ...ManagedValidator) error {
	if rs == nil {
		return fmt.Errorf("rule set is missing")
	}
	if rs.Version == "" {
		return fmt.Errorf("rule set has no version; clients cache on it, so every revision needs one")
	}
	for user := range rs.Groups {
		if user == "" {
			return fmt.Errorf("groups has an entry with no user")
		}
	}
	seen := make(map[string]bool, len(rs.Rules))
	for i, rule := range rs.Rules {
		if rule.Name == "" {
			return fmt.Errorf("rule %d has no name; names are how an applied policy is audited", i)
		}
		if seen[rule.Name] {
			return fmt.Errorf("rule %q is defined twice; names must be unique to be traceable", rule.Name)
		}
		seen[rule.Name] = true
		for agentName, config := range rule.Agents {
			if agentName == "" {
				return fmt.Errorf("rule %q configures an agent with no name", rule.Name)
			}
			if config.Launch != nil && agentName != "codex" {
				return fmt.Errorf("rule %q, agent %q: launch is not supported; %s applies managed at launch", rule.Name, agentName, agentName)
			}
			if config.Launch != nil {
				if p := FirstNull(config.Launch, "launch"); p != "" {
					return fmt.Errorf("rule %q, agent %q: %s is null, which TOML cannot represent", rule.Name, agentName, p)
				}
				if p, key := FirstBadKey(config.Launch, "launch"); p != "" {
					return fmt.Errorf("rule %q, agent %q: %s: key %q must use only letters, digits, _ and - to reach codex -c", rule.Name, agentName, p, key)
				}
			}
			for _, validate := range validators {
				if err := validate(agentName, config.Managed); err != nil {
					return fmt.Errorf("rule %q, agent %q: %w", rule.Name, agentName, err)
				}
			}
		}
	}
	return nil
}
