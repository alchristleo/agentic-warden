package managed

// KeysAsOf is the date the allowlist below was read from the Gemini CLI
// repository's docs/reference/configuration.md and cross-checked against the
// top level of SETTINGS_SCHEMA in packages/cli/src/config/settingsSchema.ts.
// A key that appears after that date is rejected until this file is
// refreshed; the error names the date so the reader knows which reference
// to compare against.
const KeysAsOf = "2026-09-22"

// SettingsKeys is every top-level key settings.json documents, sorted. Only
// the top level is checked: nested shapes are many and change often, and a
// JSON round trip catches what cannot be encoded at all. Gemini reads
// unknown keys without complaint, so a typo here would be a policy that
// silently does nothing; making it an apply-time error is the point.
var SettingsKeys = []string{
	"admin",
	"adminPolicyPaths",
	"advanced",
	"agents",
	"billing",
	"context",
	"contextManagement",
	"experimental",
	"extensions",
	"general",
	"hooks",
	"hooksConfig",
	"ide",
	"mcp",
	"mcpServers",
	"model",
	"modelConfigs",
	"output",
	"policyPaths",
	"privacy",
	"security",
	"skills",
	"telemetry",
	"tools",
	"ui",
	"useWriteTodos",
}

// Decisions is what a policy rule's decision field may say, sorted. It is
// the PolicyDecision enum in packages/core/src/policy/types.ts as of
// KeysAsOf.
var Decisions = []string{"allow", "ask_user", "deny"}
