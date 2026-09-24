import Link from "next/link";
import { site } from "@/content/site";

export function SiteFooter() {
  const year = new Date().getFullYear();
  return (
    <footer className="border-t border-border/60">
      <div className="mx-auto flex max-w-6xl flex-col gap-4 px-4 py-10 text-sm text-muted-foreground sm:flex-row sm:items-center sm:px-6">
        <nav aria-label="Footer" className="flex flex-wrap gap-5">
          {site.nav.map((item) => (
            <Link key={item.href} href={item.href} className="hover:text-foreground">
              {item.label}
            </Link>
          ))}
          <a href={site.docsUrl} className="hover:text-foreground">Docs</a>
          <a href={site.githubUrl} className="hover:text-foreground">GitHub</a>
          <a href={site.licenseUrl} className="hover:text-foreground">{site.licenseName}</a>
        </nav>
        <p className="sm:ml-auto">© {year} {site.name}</p>
      </div>
    </footer>
  );
}
