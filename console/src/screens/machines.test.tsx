import { delay, http, HttpResponse } from "msw";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { test, expect } from "vitest";
import { server } from "@/test/server";
import { renderWithProviders, expectNoAxeViolations } from "@/test/render";

const machines = [
  { id: "m1", user: "bob@example.com", name: "bob-laptop", os: "darwin", enrolledAt: "2026-09-01T00:00:00Z",
    lastSeenAt: new Date().toISOString(), lastBundleVersion: "v3", lastKeyId: "k-old" },
  { id: "m2", user: "carol@example.com", name: "", os: "linux", enrolledAt: "2026-09-01T00:00:00Z",
    lastSeenAt: "0001-01-01T00:00:00Z" },
];

test("lists machines and flags old keys and stale machines", async () => {
  server.use(http.get("/v1/machines", () => HttpResponse.json(machines)));
  const { container } = renderWithProviders(undefined, { route: "/console/machines" });
  const row = (await screen.findByText("bob-laptop")).closest("tr")!;
  expect(within(row).getByText(/old key/i)).toBeInTheDocument();
  const carol = screen.getByText("carol@example.com").closest("tr")!;
  expect(within(carol).getByText(/never seen/i)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("revoke requires typing the machine name", async () => {
  let deleted = "";
  server.use(
    http.get("/v1/machines", () => HttpResponse.json(deleted ? machines.slice(1) : machines)),
    http.delete("/v1/machines/:id", ({ params }) => { deleted = String(params.id); return new HttpResponse(null, { status: 204 }); }),
  );
  renderWithProviders(undefined, { route: "/console/machines" });
  const row = (await screen.findByText("bob-laptop")).closest("tr")!;
  await userEvent.click(within(row).getByRole("button", { name: /revoke/i }));
  const dialog = await screen.findByRole("dialog");
  const confirm = within(dialog).getByRole("button", { name: /revoke machine/i });
  expect(confirm).toBeDisabled();
  await userEvent.type(within(dialog).getByLabelText(/type bob-laptop/i), "bob-laptop");
  await userEvent.click(confirm);
  expect(deleted).toBe("m1");
  expect(await screen.findByText(/revoked bob-laptop/i)).toBeInTheDocument();
  expect(screen.queryByText("bob-laptop")).not.toBeInTheDocument();
});

// Review Focus 5.
test("treats a 404 on revoke as already revoked", async () => {
  server.use(
    http.get("/v1/machines", () => HttpResponse.json(machines)),
    http.delete("/v1/machines/:id", () => HttpResponse.json({ error: "not found" }, { status: 404 })),
  );
  renderWithProviders(undefined, { route: "/console/machines" });
  const row = (await screen.findByText("bob-laptop")).closest("tr")!;
  await userEvent.click(within(row).getByRole("button", { name: /revoke/i }));
  const dialog = await screen.findByRole("dialog");
  await userEvent.type(within(dialog).getByLabelText(/type bob-laptop/i), "bob-laptop");
  await userEvent.click(within(dialog).getByRole("button", { name: /revoke machine/i }));
  expect(await screen.findByText(/already revoked/i)).toBeInTheDocument();
});

// Final review finding 1(b): a 403 (or any other failed fetch) must not be
// hidden behind the "no machines" empty state.
test("a 403 shows an alert, not the empty-machines message", async () => {
  server.use(http.get("/v1/machines", () => HttpResponse.json({ error: "not a console admin" }, { status: 403 })));
  renderWithProviders(undefined, { route: "/console/machines" });
  expect(await screen.findByRole("alert")).toHaveTextContent(/not a console admin/i);
  expect(screen.queryByText(/no machines enrolled yet/i)).not.toBeInTheDocument();
});

// Final review finding 1(c): the empty state must not flash while the
// query is still pending.
test("the empty state does not flash while machines are loading", async () => {
  server.use(http.get("/v1/machines", async () => {
    await delay(30);
    return HttpResponse.json([]);
  }));
  renderWithProviders(undefined, { route: "/console/machines" });
  expect(screen.queryByText(/no machines enrolled yet/i)).not.toBeInTheDocument();
  expect(await screen.findByText(/no machines enrolled yet/i)).toBeInTheDocument();
});

test("a machine without a name is confirmed by its id", async () => {
  server.use(http.get("/v1/machines", () => HttpResponse.json(machines)));
  renderWithProviders(undefined, { route: "/console/machines" });
  const row = (await screen.findByText("carol@example.com")).closest("tr")!;
  await userEvent.click(within(row).getByRole("button", { name: /revoke/i }));
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).getByRole("heading", { name: "Type m2 to confirm" })).toBeInTheDocument();
  expect(within(dialog).getByLabelText(/type m2/i)).toBeInTheDocument();
});
