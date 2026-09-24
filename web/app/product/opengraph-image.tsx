import { ogSize, renderOg } from "@/lib/og";

export const alt = "Agentic Warden product overview";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("Govern coding agents like the rest of your fleet", "Targeting · Signed bundles · SCIM · Sync");
}
