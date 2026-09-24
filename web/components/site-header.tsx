import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { site } from "@/content/site";
import { buttonVariants } from "@/components/ui/button";
import { ThemeToggle } from "@/components/theme-toggle";
import { cn } from "@/lib/utils";

export function SiteHeader() {
  return (
    <header className="sticky top-0 z-40 border-b border-border/60 bg-background/80 backdrop-blur">
      <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4 sm:px-6">
        <Link href="/" className="font-mono text-sm font-semibold tracking-tight">
          {site.name}
        </Link>
        <nav aria-label="Main" className="hidden items-center gap-5 text-sm text-muted-foreground sm:flex">
          {site.nav.map((item) => (
            <Link key={item.href} href={item.href} className="hover:text-foreground">
              {item.label}
            </Link>
          ))}
          <a href={site.docsUrl} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 hover:text-foreground">
            Docs <ArrowUpRight className="size-3.5" aria-hidden />
          </a>
        </nav>
        <div className="ml-auto flex items-center gap-2">
          <ThemeToggle />
          <Link href="/demo" className={cn(buttonVariants({ size: "sm" }))}>
            Request demo
          </Link>
        </div>
      </div>
    </header>
  );
}
