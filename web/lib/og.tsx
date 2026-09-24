import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { ImageResponse } from "next/og";
import { site } from "@/content/site";

export const ogSize = { width: 1200, height: 630 };

// Satori (the renderer behind ImageResponse) can only read .ttf/.otf, not
// .woff2. The geist package ships both; read the SemiBold .ttf once at
// module scope since it doesn't depend on request data.
const geistSemiBold = await readFile(
  join(process.cwd(), "node_modules/geist/dist/fonts/geist-sans/Geist-SemiBold.ttf"),
);

export async function renderOg(title: string, subtitle: string): Promise<ImageResponse> {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: 72,
          background: "#0a0a0a",
          color: "#fafafa",
          fontFamily: "Geist",
        }}
      >
        <div style={{ fontSize: 28, opacity: 0.7, fontFamily: "monospace" }}>{site.name}</div>
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <div style={{ fontSize: 72, fontWeight: 600, lineHeight: 1.1 }}>{title}</div>
          <div style={{ fontSize: 32, opacity: 0.7 }}>{subtitle}</div>
        </div>
      </div>
    ),
    { ...ogSize, fonts: [{ name: "Geist", data: geistSemiBold, weight: 600 }] },
  );
}
