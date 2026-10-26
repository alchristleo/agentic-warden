import { ogSize, renderOg } from "@/lib/og";

export const alt = "Agentic Warden — one policy for your coding agents";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("One policy for your coding agents", "Claude Code · Codex · Antigravity CLI");
}
