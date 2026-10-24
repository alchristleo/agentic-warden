import Link from "next/link";
import type { ReactNode } from "react";
import { DocsCopy } from "@/components/docs-copy";
import { DocsNav } from "@/components/docs-nav";
import { buttonVariants } from "@/components/ui/button";
import { docHref, loadDocs, navSections } from "@/lib/docs";
import { cn } from "@/lib/utils";
import "./docs.css";

export default function DocsLayout({ children }: { children: ReactNode }) {
  const sections = navSections(loadDocs()).map((s) => ({
    ...s,
    pages: s.pages.map((p) => ({ slug: p.slug, title: p.title, href: docHref(p.slug) })),
  }));
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-10 px-4 py-12 sm:px-6 md:flex-row">
      <DocsNav sections={sections} />
      <div className="min-w-0 max-w-3xl flex-1">
        {children}
        <aside className="mt-16 flex flex-wrap items-center justify-between gap-4 rounded-xl border border-border p-5">
          <p>
            <strong>Bring your rules.</strong> See it against your own policy.
          </p>
          <Link href="/demo" className={cn(buttonVariants({ size: "sm" }))}>
            Request a demo
          </Link>
        </aside>
      </div>
      <DocsCopy />
    </div>
  );
}
