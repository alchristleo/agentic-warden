import { DemoForm } from "@/components/demo/demo-form";
import { parsePlan } from "@/lib/demo/schema";

export default async function DemoPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { plan } = await searchParams;
  // Server Component rendered per request (see searchParams above); stamping the actual
  // render time here is required so the spam timer works without JavaScript
  // (global-constraints.md ruling on /demo).
  // eslint-disable-next-line react-hooks/purity
  const startedAt = Date.now();
  return (
    <div className="mx-auto max-w-2xl px-4 py-16 sm:px-6">
      <h1 className="text-4xl font-semibold tracking-tight">Request a demo</h1>
      <p className="mt-4 text-muted-foreground">
        Tell us which agents your developers use. We will reply by email.
      </p>
      <DemoForm plan={parsePlan(plan)} startedAt={startedAt} />
    </div>
  );
}
