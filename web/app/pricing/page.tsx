import Link from "next/link";
import { Check } from "lucide-react";
import { JsonLd } from "@/components/json-ld";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { pricing } from "@/content/pricing";
import { cn } from "@/lib/utils";
import { faqJsonLd, pageMetadata } from "@/lib/seo";

export const metadata = pageMetadata("/pricing");

export default function PricingPage() {
  return (
    <div className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
      <JsonLd data={faqJsonLd()} />
      <h1 className="text-4xl font-semibold tracking-tight">{pricing.title}</h1>
      <p className="mt-4 text-lg text-muted-foreground">{pricing.lede}</p>

      <div className="mt-12 grid gap-6 md:grid-cols-3">
        {pricing.tiers.map((tier) => (
          <article key={tier.id} aria-labelledby={`tier-${tier.id}`} className="flex flex-col gap-4 rounded-xl border border-border bg-card p-6">
            <div className="flex items-center gap-2">
              <h2 id={`tier-${tier.id}`} className="text-lg font-semibold">{tier.name}</h2>
              {tier.badge ? <Badge variant="secondary">{tier.badge}</Badge> : null}
            </div>
            <p className="text-sm text-muted-foreground">{tier.summary}</p>
            <p className="text-2xl font-semibold">{tier.price}</p>
            <ul className="flex flex-col gap-2 text-sm">
              {tier.features.map((feature) => (
                <li key={feature} className="flex gap-2"><Check className="mt-0.5 size-4 shrink-0" aria-hidden />{feature}</li>
              ))}
            </ul>
            <Link href={`/demo?plan=${tier.id}`} className={cn(buttonVariants(), "mt-auto")}>
              {tier.cta}
            </Link>
          </article>
        ))}
      </div>

      <section aria-labelledby="compare-title" className="mt-20">
        <h2 id="compare-title" className="text-2xl font-semibold tracking-tight">Compare plans</h2>
        <div className="mt-6 overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Feature</TableHead>
                {pricing.comparison.columns.map((c) => <TableHead key={c} scope="col">{c}</TableHead>)}
              </TableRow>
            </TableHeader>
            <TableBody>
              {pricing.comparison.rows.map((row) => (
                <TableRow key={row.feature}>
                  <TableHead scope="row" className="font-normal">{row.feature}</TableHead>
                  {row.values.map((v, i) => <TableCell key={i}>{v}</TableCell>)}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </section>

      <section aria-labelledby="faq-title" className="mt-20 max-w-3xl">
        <h2 id="faq-title" className="text-2xl font-semibold tracking-tight">Frequently asked questions</h2>
        <div className="mt-6 divide-y divide-border border-y border-border">
          {pricing.faq.map((item) => (
            <details key={item.q} className="group py-4">
              <summary className="cursor-pointer font-medium">{item.q}</summary>
              <p className="mt-2 text-muted-foreground">{item.a}</p>
            </details>
          ))}
        </div>
      </section>
    </div>
  );
}
