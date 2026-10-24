import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { siteUrl } from "@/lib/env";
import { docHref, docSlug, loadDocs, renderMarkdown } from "@/lib/docs";

type Props = { params: Promise<{ slug?: string[] }> };

export const dynamicParams = false;

export function generateStaticParams() {
  return loadDocs().map((d) => ({ slug: docSlug(d.slug) }));
}

function find(slug: string[] = []) {
  return loadDocs().find((d) => docSlug(d.slug).join("/") === slug.join("/"));
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const doc = find((await params).slug);
  if (!doc) return {};
  const title = `${doc.title} — Agentic Warden docs`;
  return {
    title: { absolute: title },
    description: doc.description,
    alternates: { canonical: `${siteUrl()}${docHref(doc.slug)}` },
  };
}

export default async function DocsPage({ params }: Props) {
  const doc = find((await params).slug);
  if (!doc) notFound();
  return (
    <article>
      <h1 className="text-balance text-4xl font-semibold tracking-tight">{doc.title}</h1>
      <p className="mt-4 text-lg text-muted-foreground">{doc.description}</p>
      <div className="docs-prose mt-10" dangerouslySetInnerHTML={{ __html: renderMarkdown(doc.body) }} />
    </article>
  );
}
