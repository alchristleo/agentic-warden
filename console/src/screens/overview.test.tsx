import { http, HttpResponse } from "msw";
import { screen } from "@testing-library/react";
import { test, expect } from "vitest";
import { server } from "@/test/server";
import { renderWithProviders, expectNoAxeViolations } from "@/test/render";

const revisions = [
  { seq: 3, version: "v3", ruleSet: {}, createdAt: "2026-09-20T00:00:00Z", createdBy: "alice@example.com" },
  { seq: 2, version: "v2", ruleSet: {}, createdAt: "2026-09-10T00:00:00Z", createdBy: "bob@example.com" },
];

const machines = [
  { id: "m1", user: "bob@example.com", name: "bob-laptop", os: "darwin", enrolledAt: "2026-09-01T00:00:00Z",
    lastSeenAt: new Date().toISOString(), lastBundleVersion: "v3", lastKeyId: "k-old" },
  { id: "m2", user: "carol@example.com", name: "carol-desk", os: "linux", enrolledAt: "2026-09-01T00:00:00Z",
    lastSeenAt: new Date().toISOString(), lastBundleVersion: "v3", lastKeyId: "k-new" },
];

const groupsView = {
  hasSnapshot: true,
  source: "okta",
  appliedBy: "alice@example.com",
  syncedAt: new Date(Date.now() - 3600_000).toISOString(),
  members: { bob: ["devs"] },
};

const auditEvents = Array.from({ length: 5 }, (_, i) => ({
  id: 5 - i,
  at: "2026-09-24T00:00:00Z",
  actor: "alice@example.com",
  action: "policy.apply",
  target: `rev-${5 - i}`,
}));

test("shows the current policy, machine count, group source and recent activity", async () => {
  server.use(
    http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)),
    http.get("/v1/machines", () => HttpResponse.json(machines)),
    http.get("/v1/groups", () => HttpResponse.json(groupsView)),
    http.get("/v1/audit", () => HttpResponse.json(auditEvents)),
  );
  const { container } = renderWithProviders(undefined, { route: "/console/" });
  expect(await screen.findByText(/v3 by alice@example\.com/)).toBeInTheDocument();
  expect(await screen.findByText(/2 machines/)).toBeInTheDocument();
  expect(await screen.findByText(/1 machine on an old key/i)).toBeInTheDocument();
  expect(await screen.findByText(/okta/)).toBeInTheDocument();
  for (const event of auditEvents) {
    expect(await screen.findByText(new RegExp(event.target!))).toBeInTheDocument();
  }
  await expectNoAxeViolations(container);
});

test("with no revisions it shows No policy yet", async () => {
  server.use(http.get("/v1/policy/revisions", () => HttpResponse.json([])));
  renderWithProviders(undefined, { route: "/console/" });
  expect(await screen.findByText(/no policy yet/i)).toBeInTheDocument();
});
