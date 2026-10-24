import Link from "next/link";
import type { ReactNode } from "react";
import { DocsCopy } from "@/components/docs-copy";
import { docHref, loadDocs, navSections } from "@/lib/docs";
import "./docs.css";

export default function DocsLayout({ children }: { children: ReactNode }) {
  const sections = navSections(loadDocs());
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-10 px-4 py-12 sm:px-6 md:flex-row">
      <nav aria-label="Docs" className="shrink-0 md:w-56">
        {sections.map((s) => (
          <div key={s.id} className="mb-6">
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{s.label}</h2>
            <ul className="flex flex-col gap-1 text-sm">
              {s.pages.map((p) => (
                <li key={p.slug}>
                  <Link href={docHref(p.slug)} className="text-muted-foreground hover:text-foreground">
                    {p.title}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>
      <div className="min-w-0 max-w-3xl flex-1">{children}</div>
      <DocsCopy />
    </div>
  );
}
