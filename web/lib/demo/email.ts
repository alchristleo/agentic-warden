import { agentOptions, deploymentOptions, sizeOptions, type DemoRequest } from "./schema";

const label = (opts: readonly { value: string; label: string }[], value: string) =>
  opts.find((o) => o.value === value)?.label ?? value;

const oneLine = (s: string) => s.replace(/[\r\n]/g, " ");

export function formatEmail(req: DemoRequest): { subject: string; text: string } {
  const subject = oneLine(`Demo request: ${req.company} (${req.plan ?? "no plan"})`);
  const lines = [
    `Name: ${req.name}`,
    `Email: ${req.email}`,
    `Company: ${req.company}`,
    `Company size: ${label(sizeOptions, req.size)}`,
    `Agents: ${req.agents.map((a) => label(agentOptions, a)).join(", ")}`,
    `Deployment: ${label(deploymentOptions, req.deployment)}`,
    `Plan: ${req.plan ?? "none"}`,
    "",
    "Message:",
    req.message || "(none)",
  ];
  return { subject, text: lines.join("\n") };
}
