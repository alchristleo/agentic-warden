import Link from "next/link";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export const metadata = { title: "Page not found — Agentic Warden", robots: { index: false } };

export default function NotFound() {
  return (
    <section className="mx-auto flex max-w-2xl flex-col items-start gap-6 px-4 py-32 sm:px-6">
      <p className="font-mono text-sm text-muted-foreground">404</p>
      <h1 className="text-4xl font-semibold tracking-tight">Page not found</h1>
      <p className="text-muted-foreground">That page does not exist, or it moved.</p>
      <Link href="/" className={cn(buttonVariants({ variant: "outline" }))}>
        Back to home
      </Link>
    </section>
  );
}
