"use server";

import { senderFromEnv } from "@/lib/demo/sender";
import { submitDemo, type DemoState } from "@/lib/demo/submit";

export async function submitDemoAction(_prev: DemoState, form: FormData): Promise<DemoState> {
  return submitDemo(form, {
    send: senderFromEnv(),
    inbox: process.env.DEMO_INBOX || null,
    from: process.env.DEMO_FROM || (process.env.DEMO_SENDER === "fake" ? "site@example.test" : null),
    now: Date.now,
    log: (msg, detail) => console.error(msg, detail),
  });
}
