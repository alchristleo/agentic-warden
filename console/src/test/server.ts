import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";

export const me = { user: "alice@example.com", expiresAt: "2026-09-25T10:00:00Z", signingKeyId: "k-new" };

export const defaultHandlers = [
  http.get("/console/api/me", () => HttpResponse.json(me)),
  http.get("/v1/policy/revisions", () => HttpResponse.json([])),
  http.get("/v1/machines", () => HttpResponse.json([])),
  http.get("/v1/groups", () => HttpResponse.json({ error: "not found" }, { status: 404 })),
  http.get("/v1/audit", () => HttpResponse.json([])),
];

export const server = setupServer(...defaultHandlers);
