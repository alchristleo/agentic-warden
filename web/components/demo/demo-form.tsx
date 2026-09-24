"use client";

import { useActionState, useEffect, useMemo, useRef, useState } from "react";
import { submitDemoAction } from "@/app/demo/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { pricing, type PlanId } from "@/content/pricing";
import { agentOptions, deploymentOptions, isWebmail, sizeOptions } from "@/lib/demo/schema";
import type { DemoField, DemoState } from "@/lib/demo/submit";

const FIELD_ORDER: DemoField[] = ["name", "email", "company", "size", "agents", "deployment", "plan", "message"];
const nativeControl =
  "h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-xs focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-invalid:border-destructive";

function ErrorText({ field, errors }: { field: DemoField; errors: Partial<Record<DemoField, string>> }) {
  return errors[field] ? (
    <p id={`${field}-error`} className="text-sm text-destructive">
      {errors[field]}
    </p>
  ) : null;
}

export function DemoForm({ plan, startedAt }: { plan: PlanId | undefined; startedAt: number }) {
  const [state, action, pending] = useActionState<DemoState, FormData>(submitDemoAction, { status: "idle" });
  const formRef = useRef<HTMLFormElement>(null);
  const [webmail, setWebmail] = useState(false);

  const errors = useMemo(() => (state.status === "invalid" ? state.fieldErrors : {}), [state]);
  const values = state.status === "invalid" || state.status === "failed" ? state.values : {};
  const str = (k: string, fallback = "") => (typeof values[k] === "string" ? (values[k] as string) : fallback);
  const agents = Array.isArray(values.agents) ? values.agents : [];

  useEffect(() => {
    if (state.status !== "invalid") return;
    const first = FIELD_ORDER.find((f) => errors[f]);
    if (!first) return;
    const el = formRef.current?.querySelector<HTMLElement>(`[name="${first}"]`);
    el?.focus();
  }, [state, errors]);

  if (state.status === "ok") {
    return (
      <p role="status" className="mt-10 rounded-lg border border-border bg-card p-6">
        Thanks — we have your request and will reply by email.
      </p>
    );
  }

  const describedBy = (f: DemoField) => (errors[f] ? `${f}-error` : undefined);

  // key forces a remount after each server response so defaultValue picks up returned values.
  return (
    <form ref={formRef} action={action} noValidate key={JSON.stringify(values)} className="mt-10 flex flex-col gap-6">
      {state.status === "failed" ? (
        <div role="alert" className="rounded-lg border border-destructive/50 p-4 text-sm">
          Couldn&apos;t send your request.{" "}
          {state.inbox ? (
            <>
              Email us at{" "}
              <a className="underline" href={`mailto:${state.inbox}`}>
                {state.inbox}
              </a>
              .
            </>
          ) : (
            "Please try again later."
          )}
        </div>
      ) : null}

      <input type="hidden" name="startedAt" value={startedAt} />
      <div aria-hidden="true" className="absolute -left-[9999px] h-px w-px overflow-hidden">
        <label htmlFor="website">Website</label>
        <input id="website" name="website" type="text" tabIndex={-1} autoComplete="off" />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="name">Name</Label>
        <Input
          id="name"
          name="name"
          autoComplete="name"
          defaultValue={str("name")}
          aria-invalid={!!errors.name}
          aria-describedby={describedBy("name")}
        />
        <ErrorText field="name" errors={errors} />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="email">Work email</Label>
        <Input
          id="email"
          name="email"
          type="email"
          autoComplete="email"
          defaultValue={str("email")}
          aria-invalid={!!errors.email}
          aria-describedby={[describedBy("email"), webmail ? "email-hint" : undefined].filter(Boolean).join(" ") || undefined}
          onBlur={(e) => setWebmail(isWebmail(e.currentTarget.value))}
        />
        {webmail ? (
          <p id="email-hint" className="text-sm text-muted-foreground">
            A work address helps us reply faster, but this one works too.
          </p>
        ) : null}
        <ErrorText field="email" errors={errors} />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="company">Company</Label>
        <Input
          id="company"
          name="company"
          autoComplete="organization"
          defaultValue={str("company")}
          aria-invalid={!!errors.company}
          aria-describedby={describedBy("company")}
        />
        <ErrorText field="company" errors={errors} />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="size">Company size</Label>
        <select
          id="size"
          name="size"
          defaultValue={str("size")}
          className={nativeControl}
          aria-invalid={!!errors.size}
          aria-describedby={describedBy("size")}
        >
          <option value="" disabled>
            Choose…
          </option>
          {sizeOptions.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <ErrorText field="size" errors={errors} />
      </div>

      <fieldset className="flex flex-col gap-2" aria-describedby={describedBy("agents")}>
        <legend className="mb-2 text-sm font-medium">Agents your developers use</legend>
        {agentOptions.map((o) => (
          <label key={o.value} className="flex items-center gap-2 text-sm">
            <input type="checkbox" name="agents" value={o.value} defaultChecked={agents.includes(o.value)} className="size-4 accent-primary" />
            {o.label}
          </label>
        ))}
        <ErrorText field="agents" errors={errors} />
      </fieldset>

      <fieldset className="flex flex-col gap-2" aria-describedby={describedBy("deployment")}>
        <legend className="mb-2 text-sm font-medium">Deployment</legend>
        {deploymentOptions.map((o) => (
          <label key={o.value} className="flex items-center gap-2 text-sm">
            <input type="radio" name="deployment" value={o.value} defaultChecked={str("deployment") === o.value} className="size-4 accent-primary" />
            {o.label}
          </label>
        ))}
        <ErrorText field="deployment" errors={errors} />
      </fieldset>

      <div className="flex flex-col gap-2">
        <Label htmlFor="plan">Plan</Label>
        <select id="plan" name="plan" defaultValue={str("plan", plan ?? "")} className={nativeControl}>
          <option value="">Not sure yet</option>
          {pricing.tiers.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="message">Anything we should know? (optional)</Label>
        <Textarea
          id="message"
          name="message"
          rows={4}
          maxLength={2000}
          defaultValue={str("message")}
          aria-invalid={!!errors.message}
          aria-describedby={describedBy("message")}
        />
        <ErrorText field="message" errors={errors} />
      </div>

      <Button type="submit" disabled={pending} className="self-start">
        {pending ? "Sending…" : "Request demo"}
      </Button>
    </form>
  );
}
