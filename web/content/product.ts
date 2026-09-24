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
      body: "The control plane signs each machine's bundle. The sync agent verifies the signature before writing anything, so a tampered bundle never reaches an agent.",
    },
    {
      id: "scim",
      title: "SCIM provisioning",
      body: "A SCIM 2.0 endpoint accepts users and groups from Okta and Microsoft Entra ID. Provisioned groups join authored groups and snapshots when rules are resolved.",
    },
    {
      id: "sync",
      title: "Scheduled sync on every OS",
      body: "`aw-sync install-timer` installs a systemd timer on Linux, a launchd daemon on macOS or a scheduled task on Windows, and refuses to schedule a binary a non-root user could replace.",
    },
    {
      id: "fail-safe",
      title: "Claude Code always starts",
      body: "The policy helper works offline from the last synced bundle, validates its output against Claude Code's settings schema and never exits non-zero, so a bad day on the network never blocks a developer.",
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
    body: "Manage policy, groups and enrolled machines from a browser, without running the control plane yourself.",
    cta: { href: "/demo?plan=team", label: "Join early access" },
  },
} as const;
