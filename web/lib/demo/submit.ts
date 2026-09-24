import { formatEmail } from "./email";
import { demoSchema, parsePlan } from "./schema";
import type { Sender } from "./sender";

export type DemoField = "name" | "email" | "company" | "size" | "agents" | "deployment" | "plan" | "message";

type Values = Record<string, string | string[]>;

export type DemoState =
  | { status: "idle" }
  | { status: "ok" }
  | { status: "invalid"; fieldErrors: Partial<Record<DemoField, string>>; values: Values }
  | { status: "failed"; inbox: string | null; values: Values };

export type SubmitDeps = {
  send: Sender | null;
  inbox: string | null;
  from: string | null;
  now: () => number;
  log: (msg: string, detail: Record<string, unknown>) => void;
};

const MIN_FILL_MS = 3_000;
const TEXT_FIELDS = ["name", "email", "company", "size", "deployment", "plan", "message"] as const;

function readValues(form: FormData): Values {
  const values: Values = {};
  for (const key of TEXT_FIELDS) {
    const v = form.get(key);
    values[key] = typeof v === "string" ? v : "";
  }
  values.agents = form.getAll("agents").filter((v): v is string => typeof v === "string");
  return values;
}

function tooFast(form: FormData, now: number): boolean {
  const raw = form.get("startedAt");
  const started = typeof raw === "string" && raw !== "" ? Number(raw) : NaN;
  // A missing or unreadable timestamp skips the check; the honeypot still applies.
  return Number.isFinite(started) && now - started < MIN_FILL_MS;
}

export async function submitDemo(form: FormData, deps: SubmitDeps): Promise<DemoState> {
  const honeypot = form.get("website");
  if ((typeof honeypot === "string" && honeypot !== "") || tooFast(form, deps.now())) {
    return { status: "ok" };
  }

  const values = readValues(form);
  const parsed = demoSchema.safeParse({
    ...values,
    plan: parsePlan(values.plan),
    message: values.message || undefined,
  });
  if (!parsed.success) {
    const fieldErrors: Partial<Record<DemoField, string>> = {};
    for (const issue of parsed.error.issues) {
      const field = issue.path[0] as DemoField;
      fieldErrors[field] ??= issue.message;
    }
    return { status: "invalid", fieldErrors, values };
  }

  if (!deps.send || !deps.inbox || !deps.from) {
    deps.log("demo: sender not configured", {});
    return { status: "failed", inbox: deps.inbox, values };
  }

  const { subject, text } = formatEmail(parsed.data);
  try {
    await deps.send({ to: deps.inbox, from: deps.from, replyTo: parsed.data.email, subject, text });
  } catch (err) {
    const status = err && typeof err === "object" && "status" in err ? (err as { status?: unknown }).status : undefined;
    deps.log("demo: send failed", {
      error: err instanceof Error ? err.name : "unknown",
      status: typeof status === "number" ? status : undefined,
    });
    return { status: "failed", inbox: deps.inbox, values };
  }
  return { status: "ok" };
}
