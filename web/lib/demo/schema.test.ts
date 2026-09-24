import { describe, expect, it } from "vitest";
import { demoSchema, isWebmail, parsePlan } from "./schema";

const valid = {
  name: "Ada Lovelace",
  email: "ada@acme.com",
  company: "Acme",
  size: "51-500",
  agents: ["claude-code", "codex"],
  deployment: "hosted",
};

describe("demoSchema", () => {
  it("accepts a minimal valid request", () => {
    expect(demoSchema.safeParse(valid).success).toBe(true);
  });
  it("accepts optional plan and message", () => {
    expect(demoSchema.safeParse({ ...valid, plan: "team", message: "hi" }).success).toBe(true);
  });
  it.each([
    ["name", ""],
    ["name", "x".repeat(101)],
    ["email", "not-an-email"],
    ["email", `${"a".repeat(250)}@x.io`],
    ["company", ""],
    ["company", "x".repeat(101)],
    ["size", "10"],
    ["agents", []],
    ["agents", ["cursor"]],
    ["deployment", "cloud"],
    ["plan", "free"],
    ["message", "x".repeat(2001)],
  ])("rejects %s = %j", (field, value) => {
    const result = demoSchema.safeParse({ ...valid, [field]: value });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.path[0]).toBe(field);
  });
  it("trims text fields", () => {
    const result = demoSchema.parse({ ...valid, name: "  Ada  " });
    expect(result.name).toBe("Ada");
  });
  it("trims the email before validating", () => {
    const result = demoSchema.parse({ ...valid, email: " ada@acme.com " });
    expect(result.email).toBe("ada@acme.com");
  });
});

describe("isWebmail", () => {
  it.each(["a@gmail.com", "a@Outlook.com", "a@yahoo.com", "a@icloud.com", "a@proton.me"])("flags %s", (email) => {
    expect(isWebmail(email)).toBe(true);
  });
  it("does not flag a company domain", () => {
    expect(isWebmail("a@acme.com")).toBe(false);
  });
});

describe("parsePlan", () => {
  it("keeps known plans", () => {
    expect(parsePlan("enterprise")).toBe("enterprise");
  });
  it("ignores unknown values", () => {
    expect(parsePlan("free")).toBeUndefined();
    expect(parsePlan(["team"])).toBeUndefined();
    expect(parsePlan(undefined)).toBeUndefined();
  });
});
