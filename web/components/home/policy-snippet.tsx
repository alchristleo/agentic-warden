import { home } from "@/content/home";

export function PolicySnippet() {
  return (
    <figure aria-label="Policy" className="overflow-hidden rounded-lg border border-border bg-card">
      <figcaption className="border-b border-border px-4 py-2 font-mono text-xs text-muted-foreground">org-policy.yaml</figcaption>
      <pre className="overflow-x-auto p-4 font-mono text-xs leading-relaxed"><code>{home.hero.policy}</code></pre>
    </figure>
  );
}
