import { ogSize, renderOg } from "@/lib/og";

export const alt = "Request an Agentic Warden demo";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("Request a demo", "See it against your own policy");
}
