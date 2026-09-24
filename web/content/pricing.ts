export type PlanId = "team" | "enterprise" | "self-hosted";

export const planIds: readonly PlanId[] = ["team", "enterprise", "self-hosted"];

export const pricing = {
  title: "Pricing",
  lede: "Every plan starts with a conversation about the agents and rules you run today.",
  tiers: [
    {
      id: "team",
      name: "Team",
      badge: "Coming soon",
      summary: "Hosted for you. For teams that want governance without running a control plane.",
      price: "Contact us",
      features: ["Hosted console", "Claude Code, Codex and Gemini CLI", "Group and repository targeting", "Signed bundles"],
      cta: "Request demo",
    },
    {
      id: "enterprise",
      name: "Enterprise",
      badge: null,
      summary: "For organizations provisioning users from an identity provider.",
      price: "Contact us",
      features: ["Everything in Team", "SCIM provisioning (Okta, Entra ID)", "Rollout help for your policy", "Self-hosted today; hosted when the console launches"],
      cta: "Request demo",
    },
    {
      id: "self-hosted",
      name: "Self-hosted",
      badge: null,
      summary: "Apache-2.0 source. Run the control plane on your own infrastructure.",
      price: "Contact us",
      features: ["Control plane on your servers", "PostgreSQL storage", "Every agent and targeting feature", "Talk to us for rollout help"],
      cta: "Talk to us",
    },
  ] satisfies readonly {
    id: PlanId;
    name: string;
    badge: string | null;
    summary: string;
    price: string;
    features: readonly string[];
    cta: string;
  }[],
  comparison: {
    columns: ["Team", "Enterprise", "Self-hosted"],
    rows: [
      { feature: "Claude Code, Codex, Gemini CLI", values: ["Yes", "Yes", "Yes"] },
      { feature: "Group and repository targeting", values: ["Yes", "Yes", "Yes"] },
      { feature: "Signed bundles", values: ["Yes", "Yes", "Yes"] },
      { feature: "SCIM provisioning", values: ["—", "Yes", "Yes"] },
      { feature: "Hosted console", values: ["Coming soon", "Coming soon", "—"] },
      { feature: "Runs on your infrastructure", values: ["—", "Yes (hosted coming soon)", "Yes"] },
    ],
  },
  faq: [
    {
      q: "Does it replace Claude Code's managed settings?",
      a: "No. It builds on them. The sync agent writes the managed-settings drop-in that names the policy helper, and the helper computes the settings for each session.",
    },
    {
      q: "What happens if the control plane is down?",
      a: "Nothing changes for developers. The helper reads the last synced bundle on disk, so Claude Code keeps starting with the last policy it received.",
    },
    {
      q: "Can a developer bypass it by running claude directly?",
      a: "Not for Claude Code, short of local administrator rights: it runs the policy helper on every launch, wrapper or not. For Codex and Gemini CLI the machine-wide files always apply; repository-specific rules apply when launched through aw codex or aw gemini.",
    },
    {
      q: "Which identity providers work?",
      a: "Okta and Microsoft Entra ID. The endpoint speaks SCIM 2.0 and handles the request shapes those two send.",
    },
    {
      q: "Is the hosted console available?",
      a: "Not yet. Choose Team or pick hosted on the demo form and we will contact you when early access opens.",
    },
    {
      q: "How much does it cost?",
      a: "Pricing depends on the plan and the size of your organization. Request a demo and we will send a quote.",
    },
  ],
} as const;
