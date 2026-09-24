import type { MetadataRoute } from "next";
import { sitemapFor } from "@/lib/seo";

export default function sitemap(): MetadataRoute.Sitemap {
  return sitemapFor();
}
