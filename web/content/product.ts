export const product = {
  title: "Govern coding agents like the rest of your fleet",
  lede: "Agentic Warden is a control plane, a sync agent and a launch-time helper. Together they turn one policy into the right settings for every developer, repository and agent.",
  features: [
    {
      id: "targeting",
      title: "Group and repository targeting",
      body: "Rules match on identity-provider groups and on repository globs. Later rules override earlier ones per key, and permission lists union, so precedence is the order you write.",
    },
    {
      id: "signed-bundles",
      title: "Signed bundles",
      body: "When you configure a signing key, the control plane signs each machine's bundle and aw-sync verifies it against the key pinned at enrollment before writing anything, so tampering with a bundle is detected before it reaches an agent. Machines enrolled before the key was set pin nothing and need re-enrolling. Signing detects tampering; it cannot stop a root user. Details: /docs/security-model.",
    },
    {
      id: "scim",
      title: "SCIM provisioning",
      body: "A SCIM 2.0 endpoint accepts users and groups from Okta and Microsoft Entra ID. Provisioned groups join authored groups and snapshots when rules are resolved.",
    },
    {
      id: "sync",
      title: "Scheduled sync on every OS",
      body: "aw-sync install-timer installs a systemd timer on Linux, a launchd daemon on macOS or a scheduled task on Windows. On Linux and macOS it refuses to schedule a binary a non-root user could replace.",
    },
    {
      id: "fail-safe",
      title: "Fails open by default, closed when you choose",
      body: "The policy helper works offline from the last synced bundle and validates its output against Claude Code's settings schema, so a network outage never blocks a developer. On a missing or bad bundle it fails open and Claude Code falls back to the static managed files, unless you set requireBundle to refuse to start instead.",
    },
    {
      id: "doctor",
      title: "aw doctor",
      body: "One command shows each agent's compiled policy, which repository it was compiled for, when the machine last synced, and any setting that shadows the managed ones.",
    },
  ],
  console: {
    id: "console",
    title: "Hosted console",
    badge: "Coming soon",
    body: "Manage policy, groups and enrolled machines from a browser, without running the control plane yourself. Until it launches, self-host awd, whose /console/ is opt-in via AWD_PUBLIC_URL and AWD_CONSOLE_*.",
    cta: { href: "/demo?plan=team", label: "Join early access" },
  },
} as const;
