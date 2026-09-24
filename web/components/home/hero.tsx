import type { CSSProperties } from "react";
import Link from "next/link";
import { buttonVariants } from "@/components/ui/button";
import { PolicySnippet } from "@/components/home/terminal";
import { home } from "@/content/home";
import { cn } from "@/lib/utils";

export function Hero() {
  const { hero } = home;
  return (
    <section className="mx-auto grid max-w-6xl gap-12 px-4 pb-20 pt-16 sm:px-6 lg:grid-cols-2 lg:pt-24">
      <div className="flex flex-col gap-6">
        <p className="w-fit rounded-full border border-border bg-card px-3 py-1 font-mono text-xs uppercase tracking-widest text-muted-foreground">
          {hero.eyebrow}
        </p>
        <h1 className="text-balance text-4xl font-semibold tracking-tight sm:text-5xl">{hero.title}</h1>
        <p className="text-pretty text-lg text-muted-foreground">{hero.lede}</p>
        <div className="flex flex-wrap gap-3">
          <Link href={hero.primaryCta.href} className={cn(buttonVariants({ size: "lg" }))}>
            {hero.primaryCta.label}
          </Link>
          <Link href={hero.secondaryCta.href} className={cn(buttonVariants({ size: "lg", variant: "outline" }))}>
            {hero.secondaryCta.label}
          </Link>
        </div>
      </div>
      <div className="flex flex-col gap-4">
        <figure aria-label="Terminal" className="overflow-hidden rounded-lg border border-border bg-card font-mono text-sm">
          <div className="flex items-center gap-1.5 border-b border-border px-4 py-2" aria-hidden>
            <span className="size-2.5 rounded-full bg-muted-foreground/30" />
            <span className="size-2.5 rounded-full bg-muted-foreground/30" />
            <span className="size-2.5 rounded-full bg-muted-foreground/30" />
          </div>
          <div className="flex flex-col gap-2 p-4">
            {hero.terminal.map((line, i) => (
              <div
                key={line.command}
                className="motion-safe:animate-terminal-line"
                style={{ "--line-delay": `${300 + i * 500}ms` } as CSSProperties}
              >
                <div className="text-muted-foreground">{line.comment}</div>
                <div>
                  <span className="text-muted-foreground" aria-hidden>
                    ${" "}
                  </span>
                  {line.command}
                </div>
              </div>
            ))}
          </div>
        </figure>
        <PolicySnippet />
      </div>
    </section>
  );
}
