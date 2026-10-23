package requirements

// KeysAsOf is the date the allowlist below was read from OpenAI's managed
// configuration reference for Codex (learn.chatgpt.com, "Managed
// configuration", requirements.toml). A key that appears after that date is
// rejected until this file is refreshed; the error names the date so the
// reader knows which reference to compare against.
const KeysAsOf = "2026-10-23"

// Keys is every top-level key requirements.toml documents, sorted. Only the
// top level is checked: nested shapes vary by key and change more often, and
// a TOML round trip catches what cannot be encoded at all. A key missing
// here makes Codex ignore the whole file at best, so it is an apply-time
// error, on the same path as a Claude settings schema violation.
var Keys = []string{
	"additional_developer_instructions",
	"allow_appshots",
	"allow_browser_and_computer_use",
	"allow_login_shell",
	"allow_managed_hooks_only",
	"allow_remote_control",
	"allowed_approval_policies",
	"allowed_approvals_reviewers",
	"allowed_chatgpt_workspaces",
	"allowed_login_methods",
	"allowed_permission_profiles",
	"allowed_sandbox_modes",
	"allowed_web_search_modes",
	"application",
	"apps",
	"auto_review",
	"browser_use",
	"chatgpt_base_url",
	"check_for_update_on_startup",
	"cli_auth_credentials_store",
	"computer_use",
	"default_permissions",
	"enforce_residency",
	"experimental_network",
	"features",
	"feedback",
	"guardian_extra_policy",
	"guardian_policy_config",
	"hooks",
	"in_app_browser",
	"log_dir",
	"marketplaces",
	"mcp_servers",
	"model_catalog_json",
	"model_provider",
	"model_providers",
	"models",
	"permissions",
	"plugins",
	"remote_sandbox_config",
	"rules",
	"sqlite_home",
	"windows",
}
