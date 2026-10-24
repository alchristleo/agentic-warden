"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef } from "react";

export type DocsNavSection = { id: string; label: string; pages: { slug: string; title: string; href: string }[] };

/** Docs navigation: always open from md up, a collapsible menu below it. */
export function DocsNav({ sections }: { sections: DocsNavSection[] }) {
  const pathname = usePathname();
  const details = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    if (details.current) details.current.open = window.matchMedia("(min-width: 768px)").matches;
  }, [pathname]);
  return (
    <nav aria-label="Docs" className="shrink-0 md:w-56">
      <details ref={details} open>
        <summary className="cursor-pointer list-none rounded-md border border-border px-3 py-2 text-sm font-medium md:hidden">
          Docs menu
        </summary>
        <div className="mt-4 md:mt-0">
          {sections.map((s) => (
            <div key={s.id} className="mb-6">
              <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{s.label}</h2>
              <ul className="flex flex-col gap-1 text-sm">
                {s.pages.map((p) => {
                  const current = pathname === p.href;
                  return (
                    <li key={p.slug}>
                      <Link
                        href={p.href}
                        aria-current={current ? "page" : undefined}
                        className={current ? "font-medium text-foreground" : "text-muted-foreground hover:text-foreground"}
                      >
                        {p.title}
                      </Link>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>
      </details>
    </nav>
  );
}
