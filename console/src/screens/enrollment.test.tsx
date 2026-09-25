import { http, HttpResponse } from "msw";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { test, expect } from "vitest";
import { server } from "@/test/server";
import { renderWithProviders, expectNoAxeViolations } from "@/test/render";

test("creates a token and shows it once with the enroll command", async () => {
  let body: unknown;
  server.use(http.post("/v1/enrollment-tokens", async ({ request }) => {
    body = await request.json();
    return HttpResponse.json({ token: "tok-123", user: "dan@example.com", expiresAt: "2026-09-26T09:00:00Z" }, { status: 201 });
  }));
  const { container } = renderWithProviders(undefined, { route: "/console/enrollment" });
  await userEvent.type(await screen.findByLabelText(/user/i), "dan@example.com");
  await userEvent.clear(screen.getByLabelText(/valid for/i));
  await userEvent.type(screen.getByLabelText(/valid for/i), "48h");
  await userEvent.click(screen.getByRole("button", { name: /create token/i }));
  expect(body).toEqual({ user: "dan@example.com", ttl: "48h" });
  expect(await screen.findByText("tok-123")).toBeInTheDocument();
  expect(screen.getByText(/aw-sync enroll/)).toHaveTextContent("tok-123");
  expect(screen.getByText(/shown once/i)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("server validation errors are shown", async () => {
  server.use(http.post("/v1/enrollment-tokens", () =>
    HttpResponse.json({ error: "ttl must be at most 168h" }, { status: 422 })));
  renderWithProviders(undefined, { route: "/console/enrollment" });
  await userEvent.type(await screen.findByLabelText(/user/i), "dan@example.com");
  await userEvent.click(screen.getByRole("button", { name: /create token/i }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/ttl must be at most 168h/i);
});

test("the token is gone after navigating away and back", async () => {
  server.use(http.post("/v1/enrollment-tokens", () =>
    HttpResponse.json({ token: "tok-123", user: "dan@example.com", expiresAt: "2026-09-26T09:00:00Z" }, { status: 201 })));
  renderWithProviders(undefined, { route: "/console/enrollment" });
  await userEvent.type(await screen.findByLabelText(/user/i), "dan@example.com");
  await userEvent.click(screen.getByRole("button", { name: /create token/i }));
  expect(await screen.findByText("tok-123")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("link", { name: "Machines" }));
  await userEvent.click(screen.getByRole("link", { name: "Enrollment" }));
  expect(screen.queryByText("tok-123")).not.toBeInTheDocument();
});
