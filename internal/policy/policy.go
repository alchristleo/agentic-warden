// Package policy holds the organization configuration a launch applies.
//
// A Document is the unit the control plane will serve and the policy helper
// will cache. It is deliberately plain data with no dependency on the agent
// packages, so the same shape travels over HTTP, sits in a cache file, and
// feeds a launch without translation.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
)

// AgentConfig is the configuration for one agent.
type AgentConfig struct {
	// Managed is the settings document to enforce, in the agent's own schema.
	Managed map[string]any `json:"managed,omitempty"`
	// Env holds environment defaults to inject at launch.
	Env map[string]string `json:"env,omitempty"`
	// ForceEnv makes Env replace values the developer already exports.
	ForceEnv bool `json:"forceEnv,omitempty"`
}

// Document is a complete policy, keyed by agent name.
type Document struct {
	// Version identifies the policy revision, for diagnostics and caching.
	Version string                 `json:"version,omitempty"`
	Agents  map[string]AgentConfig `json:"agents,omitempty"`
	// AppliedRules names the rules that produced this document, in order.
	// It is the audit trail for why a developer's session is configured the
	// way it is.
	AppliedRules []string `json:"appliedRules,omitempty"`
}

// Agent returns the configuration for name, or a zero config when the policy
// does not mention it. It is safe to call on a nil Document, so a caller that
// failed to load a policy still gets a usable launch.
func (d *Document) Agent(name string) AgentConfig {
	if d == nil {
		return AgentConfig{}
	}
	return d.Agents[name]
}

// Load reads a policy document from disk.
func Load(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("policy: reading %s: %w", path, err)
	}
	return Parse(raw, path)
}

// Parse decodes a policy document. source names the origin for error
// messages: a file path, a URL, or the cache.
func Parse(raw []byte, source string) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("policy: parsing %s: %w", source, err)
	}
	return &doc, nil
}
