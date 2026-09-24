import Link from "next/link";
import { Hero } from "@/components/home/hero";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { home } from "@/content/home";
import { cn } from "@/lib/utils";
import { pageMetadata } from "@/lib/seo";

export const metadata = pageMetadata("/");

export default function HomePage() {
  return (
    <>
      <Hero />
      <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
        <h2 className="text-2xl font-semibold tracking-tight">{home.problem.title}</h2>
        <div className="mt-8 grid gap-4 md:grid-cols-3">
          {home.problem.items.map((item) => (
            <Card key={item.title}>
              <CardHeader>
                <CardTitle className="text-base">{item.title}</CardTitle>
              </CardHeader>
              <CardContent className="text-sm text-muted-foreground">{item.body}</CardContent>
            </Card>
          ))}
        </div>
      </section>
      <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
        <h2 className="text-2xl font-semibold tracking-tight">{home.steps.title}</h2>
        <ol className="mt-8 grid gap-6 md:grid-cols-3">
          {home.steps.items.map((step, i) => (
            <li key={step.command} className="flex flex-col gap-2">
              <span className="font-mono text-xs text-muted-foreground">
                {String(i + 1).padStart(2, "0")} · {step.command}
              </span>
              <h3 className="font-medium">{step.title}</h3>
              <p className="text-sm text-muted-foreground">{step.body}</p>
            </li>
          ))}
        </ol>
      </section>
      <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
        <h2 className="text-2xl font-semibold tracking-tight">{home.agents.title}</h2>
        <div className="mt-8 grid gap-4 md:grid-cols-3">
          {home.agents.items.map((agent) => (
            <div key={agent.name} className="rounded-lg border border-border p-5">
              <h3 className="font-medium">{agent.name}</h3>
              <p className="mt-2 text-sm text-muted-foreground">{agent.body}</p>
            </div>
          ))}
        </div>
      </section>
      <section className="mx-auto max-w-6xl px-4 py-20 sm:px-6">
        <div className="flex flex-col items-start gap-4 rounded-xl border border-border bg-card p-8">
          <h2 className="text-2xl font-semibold tracking-tight">{home.closing.title}</h2>
          <p className="text-muted-foreground">{home.closing.body}</p>
          <Link href={home.closing.cta.href} className={cn(buttonVariants())}>
            {home.closing.cta.label}
          </Link>
        </div>
      </section>
    </>
  );
}
