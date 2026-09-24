import type { Metadata, MetadataRoute } from "next";
import { pricing } from "@/content/pricing";
import { site } from "@/content/site";
import { isProductionDeploy, siteUrl, type Env } from "@/lib/env";

export const routes = [
  {
    path: "/",
    title: "Agentic Warden — one policy for every coding agent",
    description: site.description,
  },
  {
    path: "/product",
    title: "Product — Agentic Warden",
    description:
      "Group and repository targeting, signed bundles, SCIM provisioning and scheduled sync for Claude Code, Codex and Gemini CLI.",
  },
  {
    path: "/pricing",
    title: "Pricing — Agentic Warden",
    description: "Team, Enterprise and Self-hosted plans for governing AI coding agents. Contact us for a quote.",
  },
  {
    path: "/demo",
    title: "Request a demo — Agentic Warden",
    description: "See Agentic Warden applied to your own agent policy. Tell us which coding agents your developers use.",
  },
] as const;

export type RoutePath = (typeof routes)[number]["path"];

const abs = (path: string, env?: Env) => `${siteUrl(env)}${path}`;

export function pageMetadata(path: RoutePath, env: Env = process.env): Metadata {
  const route = routes.find((r) => r.path === path)!;
  const index = isProductionDeploy(env);
  return {
    title: { absolute: route.title },
    description: route.description,
    alternates: { canonical: abs(path, env) },
    robots: { index, follow: index },
    openGraph: {
      type: "website",
      siteName: site.name,
      title: route.title,
      description: route.description,
      url: abs(path, env),
    },
    twitter: { card: "summary_large_image", title: route.title, description: route.description },
  };
}

export function robotsFor(env: Env = process.env): MetadataRoute.Robots {
  if (!isProductionDeploy(env)) return { rules: [{ userAgent: "*", disallow: "/" }] };
  return { rules: [{ userAgent: "*", allow: "/" }], sitemap: abs("/sitemap.xml", env) };
}

export function sitemapFor(env: Env = process.env): MetadataRoute.Sitemap {
  return routes.map((r) => ({ url: abs(r.path, env) }));
}

export function organizationJsonLd(env: Env = process.env) {
  return {
    "@context": "https://schema.org",
    "@type": "Organization",
    name: site.name,
    url: abs("/", env),
    sameAs: [site.githubUrl],
  };
}

export function softwareJsonLd(env: Env = process.env) {
  return {
    "@context": "https://schema.org",
    "@type": "SoftwareApplication",
    name: site.name,
    description: site.description,
    url: abs("/", env),
    applicationCategory: "DeveloperApplication",
    operatingSystem: "Linux, macOS, Windows",
    license: site.licenseUrl,
  };
}

export function faqJsonLd() {
  return {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: pricing.faq.map((f) => ({
      "@type": "Question",
      name: f.q,
      acceptedAnswer: { "@type": "Answer", text: f.a },
    })),
  };
}

export function llmsTxt(env: Env = process.env): string {
  const lines = [
    `# ${site.name}`,
    "",
    `> ${site.description}`,
    "",
    "## Pages",
    "",
    ...routes.map((r) => `- [${r.title}](${abs(r.path, env)}): ${r.description}`),
    "",
    "## Docs",
    "",
    `- [README](${site.docsUrl}): architecture, installation and policy format`,
    "",
  ];
  return lines.join("\n");
}
