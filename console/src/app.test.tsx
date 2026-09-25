import { http, HttpResponse } from "msw";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { test, expect } from "vitest";
import { server } from "./test/server";
import { renderWithProviders, expectNoAxeViolations } from "./test/render";

test("signed out shows the sign-in link", async () => {
  server.use(http.get("/console/api/me", () => HttpResponse.json({ error: "sign in" }, { status: 401 })));
  const { container } = renderWithProviders();
  const link = await screen.findByRole("link", { name: /sign in/i });
  expect(link).toHaveAttribute("href", "/console/auth/login");
  await expectNoAxeViolations(container);
});

test("403 says not a console admin", async () => {
  server.use(http.get("/console/api/me", () => HttpResponse.json({ error: "not a console admin" }, { status: 403 })));
  renderWithProviders();
  expect(await screen.findByText(/not a console admin/i)).toBeInTheDocument();
});

test("503 shows the server's reason", async () => {
  server.use(http.get("/console/api/me", () =>
    HttpResponse.json({ error: "console needs group data (SCIM or awd groups apply)" }, { status: 503 })));
  renderWithProviders();
  expect(await screen.findByText(/console needs group data/i)).toBeInTheDocument();
});

test("signed in shows the user and navigation", async () => {
  renderWithProviders();
  expect(await screen.findByText("alice@example.com")).toBeInTheDocument();
  for (const name of ["Overview", "Policy", "Machines", "Enrollment", "Groups", "Audit"]) {
    expect(screen.getByRole("link", { name })).toBeInTheDocument();
  }
});

test("a 401 from any request returns to sign-in", async () => {
  let signedIn = true;
  server.use(
    http.get("/console/api/me", () => signedIn ? HttpResponse.json({ user: "alice@example.com", expiresAt: "" })
      : HttpResponse.json({ error: "sign in" }, { status: 401 })),
    http.get("/v1/machines", () => { signedIn = false; return HttpResponse.json({ error: "sign in" }, { status: 401 }); }),
  );
  renderWithProviders(undefined, { route: "/console/machines" });
  expect(await screen.findByRole("link", { name: /sign in/i })).toBeInTheDocument();
});

test("logout posts and returns to sign-in", async () => {
  // Signed in at first; POST /console/auth/logout succeeds (204) and flips
  // /console/api/me to 401 from then on, as the real backend would once the
  // session row is gone. Clicking "Sign out" should invalidate ["me"],
  // re-fetch it, get the 401, and land back on the sign-in screen.
  let signedIn = true;
  server.use(
    http.get("/console/api/me", () => signedIn
      ? HttpResponse.json({ user: "alice@example.com", expiresAt: "2026-09-25T10:00:00Z" })
      : HttpResponse.json({ error: "sign in" }, { status: 401 })),
    http.post("/console/auth/logout", () => {
      signedIn = false;
      return new HttpResponse(null, { status: 204 });
    }),
  );
  renderWithProviders();
  const signOut = await screen.findByRole("button", { name: /sign out/i });
  await userEvent.click(signOut);
  expect(await screen.findByRole("link", { name: /sign in/i })).toBeInTheDocument();
});
