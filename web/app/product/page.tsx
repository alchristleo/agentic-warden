import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { product } from "@/content/product";
import { cn } from "@/lib/utils";
import { pageMetadata } from "@/lib/seo";

export const metadata = pageMetadata("/product");

export default function ProductPage() {
  return (
    <div className="mx-auto max-w-4xl px-4 py-16 sm:px-6">
      <h1 className="text-balance text-4xl font-semibold tracking-tight">{product.title}</h1>
      <p className="mt-4 text-lg text-muted-foreground">{product.lede}</p>
      <div className="mt-12 flex flex-col gap-12">
        {product.features.map((f) => (
          <section key={f.id} id={f.id} aria-labelledby={`${f.id}-title`}>
            <h2 id={`${f.id}-title`} className="text-xl font-semibold">{f.title}</h2>
            <p className="mt-2 text-muted-foreground">{f.body}</p>
          </section>
        ))}
        <section
          id={product.console.id}
          aria-labelledby="console-title"
          className="rounded-xl border border-border bg-card p-6"
        >
          <div className="flex items-center gap-3">
            <h2 id="console-title" className="text-xl font-semibold">{product.console.title}</h2>
            <Badge variant="secondary">{product.console.badge}</Badge>
          </div>
          <p className="mt-2 text-muted-foreground">{product.console.body}</p>
          <Link href={product.console.cta.href} className={cn(buttonVariants({ variant: "outline" }), "mt-4")}>
            {product.console.cta.label}
          </Link>
        </section>
      </div>
    </div>
  );
}
