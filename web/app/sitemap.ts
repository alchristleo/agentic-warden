import type { MetadataRoute } from "next";
import { docHref, loadDocs } from "@/lib/docs";
import { sitemapFor } from "@/lib/seo";

export default function sitemap(): MetadataRoute.Sitemap {
  return sitemapFor(process.env, loadDocs().map((d) => docHref(d.slug)));
}
