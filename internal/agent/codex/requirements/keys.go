package requirements

// KeysAsOf is the date the allowlist below was read from OpenAI's managed
// configuration reference for Codex (learn.chatgpt.com, "Managed
// configuration", requirements.toml). A key that appears after that date is
// rejected until this file is refreshed; the error names the date so the
// reader knows which reference to compare against.
const KeysAsOf = "2026-09-22"

// Keys is every top-level key requirements.toml documents, sorted. Only the
// top level is checked: nested shapes vary by key and change more often, and
// a TOML round trip catches what cannot be encoded at all. A key missing
// here makes Codex ignore the whole file at best, so it is an apply-time
// error, on the same path as a Claude settings schema violation.
var Keys = []string{
	"allow_appshots",
	"allow_locked_computer_use",
	"allow_managed_hooks_only",
	"allow_remote_control",
	"allowed_approval_policies",
	"allowed_approvals_reviewers",
	"allowed_chatgpt_workspaces",
	"allowed_login_methods",
	"allowed_permission_profiles",
	"allowed_sandbox_modes",
	"allowed_web_search_modes",
	"browser_use",
	"chatgpt_base_url",
	"cli_auth_credentials_store",
	"computer_use",
	"default_permissions",
	"experimental_network",
	"features",
	"guardian_policy_config",
	"hooks",
	"marketplaces",
	"mcp_servers",
	"permissions",
	"remote_sandbox_config",
	"rules",
}
