import { Resend } from "resend";
import type { Env } from "@/lib/env";

export type Sender = (msg: {
  to: string;
  from: string;
  replyTo: string;
  subject: string;
  text: string;
}) => Promise<void>;

export class SendError extends Error {
  constructor(
    message: string,
    readonly status?: number,
  ) {
    super(message);
    this.name = "SendError";
  }
}

// Test-only: selected by DEMO_SENDER=fake, which assertBuildEnv forbids in
// production. Fails for the company "__fail__" so e2e tests can reach the
// fallback path.
export const fakeSender: Sender = async (msg) => {
  if (msg.subject.includes("Demo request: __fail__ ")) {
    throw new SendError("fake sender failure", 503);
  }
};

function resendSender(apiKey: string): Sender {
  const resend = new Resend(apiKey);
  return async ({ to, from, replyTo, subject, text }) => {
    const { error } = await resend.emails.send({ to, from, replyTo, subject, text });
    if (error) {
      throw new SendError(error.name, error.statusCode ?? undefined);
    }
  };
}

export function senderFromEnv(env: Env = process.env): Sender | null {
  if (env.DEMO_SENDER === "fake" && env.VERCEL_ENV !== "production") return fakeSender;
  if (env.RESEND_API_KEY) return resendSender(env.RESEND_API_KEY);
  return null;
}
