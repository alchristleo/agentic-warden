// Package policy holds the organization configuration a launch applies.
//
// A Document is the unit the control plane will serve. The compiled document
// is what `aw` applies, and it is also what `aw-policy` computes per launch
// from the bundle aw-sync leaves on the machine, rather than caching it. It
// is deliberately plain data with no dependency on the agent packages, so
// the same shape travels over HTTP and feeds a launch without translation.
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
	// Launch is the document the agent's wrapper applies per launch, in the
	// agent's launch-time schema, for an agent whose launch channel takes a
	// different shape from its managed file. Codex's requirements.toml and
	// its config.toml are two schemas; Launch is the second. Claude and
	// Gemini apply Managed at launch and have no use for it.
	Launch map[string]any `json:"launch,omitempty"`
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
