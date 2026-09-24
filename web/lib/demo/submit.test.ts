import { describe, expect, it, vi } from "vitest";
import { SendError, type Sender } from "./sender";
import { submitDemo, type SubmitDeps } from "./submit";

const T0 = 1_000_000;

function form(overrides: Record<string, string | string[] | null> = {}): FormData {
  const base: Record<string, string | string[]> = {
    name: "Ada",
    email: "ada@acme.com",
    company: "Acme",
    size: "51-500",
    agents: ["claude-code", "codex"],
    deployment: "hosted",
    plan: "team",
    message: "",
    website: "",
    startedAt: String(T0),
  };
  const fd = new FormData();
  for (const [k, v] of Object.entries({ ...base, ...overrides })) {
    if (v === null) continue;
    for (const item of Array.isArray(v) ? v : [v]) fd.append(k, item);
  }
  return fd;
}

function deps(over: Partial<SubmitDeps> = {}): SubmitDeps & { sent: Parameters<Sender>[0][]; logs: unknown[][] } {
  const sent: Parameters<Sender>[0][] = [];
  const logs: unknown[][] = [];
  return {
    send: async (m) => { sent.push(m); },
    inbox: "sales@example.test",
    from: "site@example.test",
    now: () => T0 + 10_000,
    log: (msg, detail) => { logs.push([msg, detail]); },
    ...over,
    sent,
    logs,
  };
}

describe("submitDemo", () => {
  it("sends one email and returns ok", async () => {
    const d = deps();
    expect(await submitDemo(form(), d)).toEqual({ status: "ok" });
    expect(d.sent).toHaveLength(1);
    expect(d.sent[0]).toMatchObject({
      to: "sales@example.test",
      from: "site@example.test",
      replyTo: "ada@acme.com",
      subject: "Demo request: Acme (team)",
    });
  });

  it("returns field errors and the entered values when invalid", async () => {
    const d = deps();
    const state = await submitDemo(form({ email: "nope", agents: null }), d);
    expect(state.status).toBe("invalid");
    if (state.status !== "invalid") throw new Error("unreachable");
    expect(Object.keys(state.fieldErrors).sort()).toEqual(["agents", "email"]);
    expect(state.values.email).toBe("nope");
    expect(state.values.company).toBe("Acme");
    expect(d.sent).toHaveLength(0);
  });

  it("drops an unknown plan instead of failing", async () => {
    const d = deps();
    expect(await submitDemo(form({ plan: "free" }), d)).toEqual({ status: "ok" });
    expect(d.sent[0]?.subject).toBe("Demo request: Acme (no plan)");
  });

  it("silently drops a filled honeypot", async () => {
    const d = deps();
    expect(await submitDemo(form({ website: "http://spam" }), d)).toEqual({ status: "ok" });
    expect(d.sent).toHaveLength(0);
  });

  it("silently drops a submission under 3 seconds", async () => {
    const d = deps({ now: () => T0 + 2_999 });
    expect(await submitDemo(form(), d)).toEqual({ status: "ok" });
    expect(d.sent).toHaveLength(0);
  });

  it("accepts exactly 3 seconds", async () => {
    const d = deps({ now: () => T0 + 3_000 });
    await submitDemo(form(), d);
    expect(d.sent).toHaveLength(1);
  });

  it("skips the time check when the timestamp is missing or garbage", async () => {
    for (const startedAt of [null, "", "abc"]) {
      const d = deps();
      await submitDemo(form({ startedAt }), d);
      expect(d.sent).toHaveLength(1);
    }
  });

  it("fails with the inbox and values when the sender throws, logging no field values", async () => {
    const d = deps({ send: async () => { throw new SendError("rate_limit_exceeded", 429); } });
    const state = await submitDemo(form(), d);
    expect(state).toMatchObject({ status: "failed", inbox: "sales@example.test" });
    if (state.status !== "failed") throw new Error("unreachable");
    expect(state.values.name).toBe("Ada");
    expect(d.logs).toEqual([["demo: send failed", { error: "SendError", status: 429 }]]);
    const logged = JSON.stringify(d.logs);
    for (const secret of ["Ada", "ada@acme.com", "Acme"]) expect(logged).not.toContain(secret);
  });

  it("fails when the sender is not configured", async () => {
    const log = vi.fn();
    const state = await submitDemo(form(), deps({ send: null, log }));
    expect(state.status).toBe("failed");
    expect(log).toHaveBeenCalledWith("demo: sender not configured", {});
  });

  it("fails when inbox or from is not configured", async () => {
    expect((await submitDemo(form(), deps({ inbox: null }))).status).toBe("failed");
    expect((await submitDemo(form(), deps({ from: null }))).status).toBe("failed");
  });
});
