export const home = {
  hero: {
    eyebrow: "Claude Code · Codex · Gemini CLI",
    title: "One policy for every coding agent",
    lede:
      "Write your organization's rules once. Every developer session gets the slice that applies to their groups and the repository they are in. For Claude Code it applies at launch, even when nobody types the wrapper.",
    primaryCta: { href: "/demo", label: "Request demo" },
    secondaryCta: { href: "/product", label: "See how it works" },
    terminal: [
      { comment: "# on the control plane", command: "awd apply org-policy.yaml" },
      { comment: "# once per machine, as root", command: "aw-sync install-timer" },
      { comment: "# developers keep typing what they type", command: "claude" },
    ],
    policy: `rules:
  - name: baseline
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Read(./.env)
          allowManagedPermissionRulesOnly: true
  - name: payments-repos
    match:
      repos: ["github.com/acme/payments*"]
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Bash(curl *)`,
  },
  problem: {
    title: "What the vendor consoles leave uncovered",
    items: [
      {
        title: "Rules per repository",
        body: "Managed settings apply machine-wide. A payments repository and a sandbox get the same rules unless something targets the repository itself.",
      },
      {
        title: "Rules per group on seat plans",
        body: "Organizations on per-seat plans cannot give the platform team one policy and contractors another from the vendor console.",
      },
      {
        title: "More than one agent",
        body: "Teams run Claude Code, Codex and Gemini CLI side by side. Each has its own settings format, and none reads the others'.",
      },
    ],
  },
  steps: {
    title: "How it works",
    items: [
      {
        command: "awd",
        title: "Author one policy",
        body: "The control plane holds your rules, your groups and your SCIM-provisioned users, and builds a bundle for each enrolled machine — signed, when you configure a signing key.",
      },
      {
        command: "aw-sync",
        title: "Deliver it to every machine",
        body: "A root-owned timer fetches the machine's signed bundle and writes each agent's managed settings in the agent's own format.",
      },
      {
        command: "policyHelper",
        title: "Enforce it at launch",
        body: "Claude Code runs the helper on every start, including a bare claude, and the helper applies the rules for the repository the session is in.",
      },
    ],
  },
  agents: {
    title: "Governs the agents your developers use",
    items: [
      { name: "Claude Code", body: "Managed settings and a policy helper computed per repository at every launch." },
      { name: "Codex", body: "A machine-wide requirements.toml, plus repository rules through aw codex." },
      { name: "Gemini CLI", body: "System settings and an admin policy file, plus repository rules through aw gemini." },
    ],
  },
  closing: {
    title: "See it against your own policy",
    body: "Bring the rules you enforce today. We will show you how they map to groups and repositories.",
    cta: { href: "/demo", label: "Request demo" },
  },
} as const;
