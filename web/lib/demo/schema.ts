import { z } from "zod";
import { planIds, type PlanId } from "@/content/pricing";

export const sizeOptions = [
  { value: "1-50", label: "1–50" },
  { value: "51-500", label: "51–500" },
  { value: "500+", label: "500+" },
] as const;

export const agentOptions = [
  { value: "claude-code", label: "Claude Code" },
  { value: "codex", label: "Codex" },
  { value: "gemini-cli", label: "Gemini CLI" },
  { value: "other", label: "Other" },
] as const;

export const deploymentOptions = [
  { value: "self-hosted", label: "Self-hosted" },
  { value: "hosted", label: "Hosted by us" },
  { value: "unsure", label: "Not sure yet" },
] as const;

const values = <T extends readonly { value: string }[]>(opts: T) =>
  opts.map((o) => o.value) as [T[number]["value"], ...T[number]["value"][]];

export const demoSchema = z.object({
  name: z.string().trim().min(1, "Enter your name.").max(100, "Use at most 100 characters."),
  email: z.email("Enter a valid email address.").trim().max(254, "Use at most 254 characters."),
  company: z.string().trim().min(1, "Enter your company.").max(100, "Use at most 100 characters."),
  size: z.enum(values(sizeOptions), { error: "Choose a company size." }),
  agents: z.array(z.enum(values(agentOptions))).min(1, "Choose at least one agent."),
  deployment: z.enum(values(deploymentOptions), { error: "Choose a deployment." }),
  plan: z.enum(planIds as [PlanId, ...PlanId[]]).optional(),
  message: z.string().trim().max(2000, "Use at most 2000 characters.").optional(),
});

export type DemoRequest = z.infer<typeof demoSchema>;

const WEBMAIL = new Set(["gmail.com", "outlook.com", "yahoo.com", "icloud.com", "proton.me"]);

export function isWebmail(email: string): boolean {
  const domain = email.split("@").pop()?.trim().toLowerCase() ?? "";
  return WEBMAIL.has(domain);
}

export function parsePlan(value: unknown): PlanId | undefined {
  return typeof value === "string" && (planIds as readonly string[]).includes(value)
    ? (value as PlanId)
    : undefined;
}
