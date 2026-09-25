import { test, expect } from "vitest";
import { parsePolicy, toYaml } from "./yaml";

test("round trips a rule set", () => {
  const rs = { version: "v2", rules: [{ name: "baseline", agents: { claude: { managed: { model: "sonnet" } } } }] };
  const parsed = parsePolicy(toYaml(rs));
  expect(parsed).toEqual({ ok: true, value: rs });
});

test("reports a syntax error with its line", () => {
  const r = parsePolicy("version: v1\nrules:\n  - name: [unclosed\n");
  expect(r.ok).toBe(false);
  if (!r.ok) expect(r.line).toBe(3);
});

test("rejects duplicate keys like the CLI's strict parse", () => {
  const r = parsePolicy("version: v1\nversion: v2\n");
  expect(r.ok).toBe(false);
  if (!r.ok) expect(r.line).toBe(2);
});

test("an empty document is an error, not null", () => {
  expect(parsePolicy("   \n").ok).toBe(false);
});
