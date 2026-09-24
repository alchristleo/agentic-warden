import { describe, expect, it } from "vitest";
import { SendError, fakeSender, senderFromEnv } from "./sender";

const msg = { to: "in@x.test", from: "site@x.test", replyTo: "ada@acme.com", subject: "s", text: "t" };

describe("senderFromEnv", () => {
  it("is null without a Resend key", () => {
    expect(senderFromEnv({})).toBeNull();
  });
  it("returns the fake when DEMO_SENDER=fake", () => {
    expect(senderFromEnv({ DEMO_SENDER: "fake" })).toBe(fakeSender);
  });
  it("returns a Resend sender when a key is set", () => {
    expect(typeof senderFromEnv({ RESEND_API_KEY: "re_x" })).toBe("function");
  });
});

describe("fakeSender", () => {
  it("succeeds normally", async () => {
    await expect(fakeSender(msg)).resolves.toBeUndefined();
  });
  it("fails when the subject names the __fail__ company", async () => {
    await expect(fakeSender({ ...msg, subject: "Demo request: __fail__ (no plan)" })).rejects.toBeInstanceOf(SendError);
  });
});
