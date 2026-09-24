import { ogSize, renderOg } from "@/lib/og";

export const alt = "Agentic Warden — one policy for every coding agent";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("One policy for every coding agent", "Claude Code · Codex · Gemini CLI");
}
