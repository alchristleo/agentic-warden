import { http, HttpResponse } from "msw";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { test, expect } from "vitest";
import { server } from "@/test/server";
import { renderWithProviders, expectNoAxeViolations } from "@/test/render";

const snapshot = {
  hasSnapshot: true,
  source: "okta",
  appliedBy: "alice@example.com",
  syncedAt: new Date(Date.now() - 3600_000).toISOString(),
  members: { a: ["devs", "ops"], b: ["devs"] },
};

test("renders the group table, source and last sync from a snapshot", async () => {
  server.use(http.get("/v1/groups", () => HttpResponse.json(snapshot)));
  const { container } = renderWithProviders(undefined, { route: "/console/groups" });
  const devsRow = (await screen.findByText("devs")).closest("tr")!;
  expect(within(devsRow).getByText("2")).toBeInTheDocument();
  const opsRow = screen.getByText("ops").closest("tr")!;
  expect(within(opsRow).getByText("1")).toBeInTheDocument();
  expect(screen.getByText(/okta/)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("SCIM counts render as a summary line", async () => {
  server.use(http.get("/v1/groups", () => HttpResponse.json({
    ...snapshot,
    scim: { users: 3, activeUsers: 2, groups: 1 },
  })));
  renderWithProviders(undefined, { route: "/console/groups" });
  expect(await screen.findByText(/SCIM: 3 users \(2 active\), 1 group/)).toBeInTheDocument();
});

test("a 404 from /v1/groups shows a hint about SCIM and awd groups apply", async () => {
  server.use(http.get("/v1/groups", () => HttpResponse.json({ error: "not found" }, { status: 404 })));
  const { container } = renderWithProviders(undefined, { route: "/console/groups" });
  expect(await screen.findByText(/no group data yet/i)).toBeInTheDocument();
  expect(screen.getByText(/scim/i)).toBeInTheDocument();
  expect(screen.getByText(/awd groups apply/)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("the resolve form fetches and renders each group source", async () => {
  let requestedUrl = "";
  server.use(
    http.get("/v1/groups", () => HttpResponse.json({ error: "not found" }, { status: 404 })),
    http.get("/v1/groups/resolve", ({ request }) => {
      requestedUrl = request.url;
      return HttpResponse.json({
        user: "eve@example.com",
        authored: ["devs"],
        snapshot: ["security"],
        scim: ["ops"],
        effective: ["devs", "ops", "security"],
        scimNearMatch: null,
      });
    }),
  );
  const { container } = renderWithProviders(undefined, { route: "/console/groups" });
  await userEvent.type(await screen.findByLabelText(/user/i), "eve@example.com");
  await userEvent.click(screen.getByRole("button", { name: /resolve/i }));
  await screen.findByText(/devs, ops, security/);
  const values = Array.from(container.querySelectorAll("dd")).map((el) => el.textContent);
  expect(values).toEqual(["devs", "security", "ops", "devs, ops, security"]);
  expect(requestedUrl).toContain("/v1/groups/resolve?user=" + encodeURIComponent("eve@example.com"));
  await expectNoAxeViolations(container);
});

test("scimNearMatch shows a warning naming the near match", async () => {
  server.use(
    http.get("/v1/groups", () => HttpResponse.json({ error: "not found" }, { status: 404 })),
    http.get("/v1/groups/resolve", () => HttpResponse.json({
      user: "Eve@example.com",
      authored: [],
      snapshot: [],
      scim: [],
      effective: [],
      scimNearMatch: "eve@example.com",
    })),
  );
  renderWithProviders(undefined, { route: "/console/groups" });
  await userEvent.type(await screen.findByLabelText(/user/i), "Eve@example.com");
  await userEvent.click(screen.getByRole("button", { name: /resolve/i }));
  const warning = await screen.findByRole("status");
  expect(warning).toHaveTextContent(/eve@example.com/i);
  expect(warning).toHaveTextContent(/differs only in case/i);
});
