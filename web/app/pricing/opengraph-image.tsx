import { ogSize, renderOg } from "@/lib/og";

export const alt = "Agentic Warden pricing";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("Pricing", "Team · Enterprise · Self-hosted");
}
