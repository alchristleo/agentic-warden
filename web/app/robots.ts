import type { MetadataRoute } from "next";
import { robotsFor } from "@/lib/seo";

export default function robots(): MetadataRoute.Robots {
  return robotsFor();
}
