import { readFile } from "node:fs/promises";
import { assetPath, assetType } from "@/lib/docs";

export async function GET(_req: Request, { params }: { params: Promise<{ file: string }> }) {
  const { file } = await params;
  const path = assetPath(file);
  if (!path) return new Response("Not found", { status: 404 });
  try {
    return new Response(new Uint8Array(await readFile(path)), {
      headers: { "Content-Type": assetType(file)!, "Cache-Control": "public, max-age=3600" },
    });
  } catch {
    return new Response("Not found", { status: 404 });
  }
}
