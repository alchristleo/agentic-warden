// Seeds the real `awd` server (started by playwright.config.ts's webServer)
// through its admin bearer token before any test runs. awd uses the memory
// store, so a fresh `pnpm test:e2e` run starts empty; with
// `reuseExistingServer` (the local, non-CI path) the same server can carry
// state from a previous run, so creating the "v1" revision a second time is
// tolerated as a 409 rather than failing the whole suite.
const AWD = "http://127.0.0.1:9401";

const headers = {
  Authorization: "Bearer e2e-admin",
  "X-Applied-By": "e2e",
  "Content-Type": "application/json",
};

async function seed(path: string, body: unknown, okStatuses: number[]): Promise<void> {
  const res = await fetch(`${AWD}${path}`, {
    method: path === "/v1/groups" ? "PUT" : "POST",
    headers,
    body: JSON.stringify(body),
  });
  if (!okStatuses.includes(res.status)) {
    const text = await res.text().catch(() => "");
    throw new Error(`seeding ${path}: status ${res.status}: ${text}`);
  }
}

export default async function globalSetup(): Promise<void> {
  await seed(
    "/v1/groups",
    { source: "e2e", members: { "admin@example.com": ["console-admins"], "dev@example.com": ["devs"] } },
    [200, 201, 204],
  );

  // Tolerate 409: a reused local server already has the "v1" revision from
  // an earlier run.
  await seed("/v1/policy/revisions", { version: "v1", rules: [{ name: "baseline" }] }, [200, 201, 409]);

  const tokenRes = await fetch(`${AWD}/v1/enrollment-tokens`, {
    method: "POST",
    headers,
    body: JSON.stringify({ user: "dev@example.com" }),
  });
  if (!tokenRes.ok) {
    throw new Error(`seeding /v1/enrollment-tokens: status ${tokenRes.status}`);
  }
  const { token } = (await tokenRes.json()) as { token: string };

  const enrollRes = await fetch(`${AWD}/v1/machines/enroll`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token, name: "e2e-laptop", os: "linux" }),
  });
  if (!enrollRes.ok) {
    const text = await enrollRes.text().catch(() => "");
    throw new Error(`seeding /v1/machines/enroll: status ${enrollRes.status}: ${text}`);
  }
}
