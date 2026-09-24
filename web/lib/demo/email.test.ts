import { describe, expect, it } from "vitest";
import { formatEmail } from "./email";

const req = {
  name: "Ada",
  email: "ada@acme.com",
  company: "Acme",
  size: "51-500" as const,
  agents: ["claude-code" as const, "gemini-cli" as const],
  deployment: "hosted" as const,
};

describe("formatEmail", () => {
  it("names the company and plan in the subject", () => {
    expect(formatEmail({ ...req, plan: "team" }).subject).toBe("Demo request: Acme (team)");
    expect(formatEmail(req).subject).toBe('Demo request: Acme (no plan)');
  });
  it("lists every field in the body with labels", () => {
    const { text } = formatEmail({ ...req, message: "Line one\nLine two" });
    expect(text).toContain("Name: Ada");
    expect(text).toContain("Email: ada@acme.com");
    expect(text).toContain("Company size: 51–500");
    expect(text).toContain("Agents: Claude Code, Gemini CLI");
    expect(text).toContain("Deployment: Hosted by us");
    expect(text).toContain("Line one\nLine two");
  });
  it("strips newlines from the subject", () => {
    expect(formatEmail({ ...req, company: "Acme\r\nBcc: x@y.z" }).subject).toBe("Demo request: Acme  Bcc: x@y.z (no plan)");
  });
});
