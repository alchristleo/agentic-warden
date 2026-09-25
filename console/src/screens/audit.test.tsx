import { http, HttpResponse } from "msw";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { test, expect } from "vitest";
import { server } from "@/test/server";
import { renderWithProviders, expectNoAxeViolations } from "@/test/render";

const firstPage = Array.from({ length: 50 }, (_, i) => ({
  id: 100 - i,
  at: "2026-09-20T10:00:00Z",
  actor: i === 0 ? "bob@example.com" : "dave@example.com",
  action: i === 0 ? "machine.revoke" : "policy.apply",
  target: i === 0 ? "m1" : "rev-1",
}));

const secondPage = [
  { id: 49, at: "2026-09-19T10:00:00Z", actor: "carol@example.com", action: "groups.sync", target: "okta" },
];

test("renders time, actor, action and target for each row", async () => {
  server.use(http.get("/v1/audit", () => HttpResponse.json(firstPage)));
  const { container } = renderWithProviders(undefined, { route: "/console/audit" });
  const row = (await screen.findByText("bob@example.com")).closest("tr")!;
  expect(within(row).getByText("machine.revoke")).toBeInTheDocument();
  expect(within(row).getByText("m1")).toBeInTheDocument();
  expect(within(row).getByText(/2026/)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("older requests the next page with limit and before, and appends it", async () => {
  let requestedUrl = "";
  server.use(http.get("/v1/audit", ({ request }) => {
    requestedUrl = request.url;
    const url = new URL(request.url);
    return HttpResponse.json(url.searchParams.has("before") ? secondPage : firstPage);
  }));
  renderWithProviders(undefined, { route: "/console/audit" });
  await screen.findByText("bob@example.com");
  await userEvent.click(screen.getByRole("button", { name: /older/i }));
  await screen.findByText("carol@example.com");
  const url = new URL(requestedUrl);
  expect(url.searchParams.get("limit")).toBe("50");
  expect(url.searchParams.get("before")).toBe("51");
  // Both pages are visible: the first page's rows were not replaced.
  expect(screen.getByText("bob@example.com")).toBeInTheDocument();
});

test("the actor filter and action select filter the loaded rows", async () => {
  server.use(http.get("/v1/audit", () => HttpResponse.json(firstPage)));
  renderWithProviders(undefined, { route: "/console/audit" });
  await screen.findByText("bob@example.com");
  expect(screen.getAllByText("dave@example.com")).toHaveLength(49);

  await userEvent.type(screen.getByLabelText(/actor/i), "bob");
  expect(screen.getByText("bob@example.com")).toBeInTheDocument();
  expect(screen.queryByText("dave@example.com")).not.toBeInTheDocument();

  await userEvent.clear(screen.getByLabelText(/actor/i));
  await screen.findAllByText("dave@example.com");
  await userEvent.selectOptions(screen.getByLabelText(/action/i), "machine.revoke");
  expect(screen.getByText("bob@example.com")).toBeInTheDocument();
  expect(screen.queryByText("dave@example.com")).not.toBeInTheDocument();
});

test("an empty list shows no audit events", async () => {
  server.use(http.get("/v1/audit", () => HttpResponse.json([])));
  const { container } = renderWithProviders(undefined, { route: "/console/audit" });
  expect(await screen.findByText(/no audit events/i)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

// Final review finding 1(b): a 500 must show an alert with the server's
// message, not the "no audit events" empty state.
test("a 500 shows an alert, not the empty-audit message", async () => {
  server.use(http.get("/v1/audit", () => HttpResponse.json({ error: "audit store unavailable" }, { status: 500 })));
  const { container } = renderWithProviders(undefined, { route: "/console/audit" });
  expect(await screen.findByRole("alert")).toHaveTextContent(/audit store unavailable/i);
  expect(screen.queryByText(/no audit events/i)).not.toBeInTheDocument();
  await expectNoAxeViolations(container);
});
