import { delay, http, HttpResponse } from "msw";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { test, expect } from "vitest";
import { server } from "@/test/server";
import { renderWithProviders, expectNoAxeViolations } from "@/test/render";

const revisions = [
  { seq: 2, version: "v2", ruleSet: { version: "v2", rules: [{ name: "baseline" }] }, createdAt: "2026-09-24T10:00:00Z", createdBy: "alice@example.com" },
  { seq: 1, version: "v1", ruleSet: { version: "v1" }, createdAt: "2026-09-20T10:00:00Z", createdBy: "token:ops" },
];

test("lists revisions and diffs two of them", async () => {
  server.use(http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)));
  const { container } = renderWithProviders(undefined, { route: "/console/policy" });
  expect(await screen.findByText("v2")).toBeInTheDocument();
  expect(screen.getByText("token:ops")).toBeInTheDocument();
  await userEvent.selectOptions(screen.getByLabelText(/compare from/i), "v1");
  await userEvent.selectOptions(screen.getByLabelText(/compare to/i), "v2");
  expect(await screen.findByText(/\+ .*name: baseline/)).toBeInTheDocument();
  await expectNoAxeViolations(container);
});

test("a YAML error is shown at its line and nothing is sent", async () => {
  let posted = false;
  server.use(
    http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)),
    http.post("/v1/policy/revisions", () => { posted = true; return HttpResponse.json({}, { status: 201 }); }),
  );
  renderWithProviders(undefined, { route: "/console/policy" });
  await userEvent.click(await screen.findByRole("button", { name: /new revision/i }));
  const editor = await screen.findByRole("textbox", { name: /policy yaml/i });
  await userEvent.clear(editor);
  // userEvent's keyboard syntax treats bare "[" and "{" as the start of a
  // special-key descriptor; "[[" / "{{" is how you type them literally. The
  // typed text ends up as "version: v3\nrules: [{unclosed" either way.
  await userEvent.type(editor, "version: v3\nrules: [[{{unclosed");
  await userEvent.click(screen.getByRole("button", { name: /review changes/i }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/line 2/i);
  expect(posted).toBe(false);
});

test("review shows the diff, confirm posts JSON", async () => {
  let body: unknown;
  server.use(
    http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)),
    http.post("/v1/policy/revisions", async ({ request }) => { body = await request.json(); return HttpResponse.json({}, { status: 201 }); }),
  );
  renderWithProviders(undefined, { route: "/console/policy" });
  await userEvent.click(await screen.findByRole("button", { name: /new revision/i }));
  const editor = await screen.findByRole("textbox", { name: /policy yaml/i });
  await userEvent.clear(editor);
  await userEvent.type(editor, "version: v3\nrules:\n  - name: baseline\n");
  await userEvent.click(screen.getByRole("button", { name: /review changes/i }));
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).getByText(/- version: v2/)).toBeInTheDocument();
  expect(within(dialog).getByText(/\+ version: v3/)).toBeInTheDocument();
  await userEvent.click(within(dialog).getByRole("button", { name: /apply revision/i }));
  expect(body).toEqual({ version: "v3", rules: [{ name: "baseline" }] });
  expect(await screen.findByText(/applied v3/i)).toBeInTheDocument();
});

test("a 422 from the server is shown above the editor and the draft is kept", async () => {
  server.use(
    http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)),
    http.post("/v1/policy/revisions", () =>
      HttpResponse.json({ error: "rule baseline: agent claude: managed.model must be a string" }, { status: 422 }),
    ),
  );
  renderWithProviders(undefined, { route: "/console/policy" });
  await userEvent.click(await screen.findByRole("button", { name: /new revision/i }));
  const editor = await screen.findByRole("textbox", { name: /policy yaml/i });
  await userEvent.clear(editor);
  await userEvent.type(editor, "version: v3\nrules:\n  - name: baseline\n");
  await userEvent.click(screen.getByRole("button", { name: /review changes/i }));
  const dialog = await screen.findByRole("dialog");
  await userEvent.click(within(dialog).getByRole("button", { name: /apply revision/i }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/managed\.model must be a string/i);
  expect(screen.getByRole("textbox", { name: /policy yaml/i })).toHaveValue("version: v3\nrules:\n  - name: baseline\n");
});

// Review Focus 4.
test("shows the server message when the version is not a string", async () => {
  server.use(
    http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)),
    http.post("/v1/policy/revisions", () =>
      HttpResponse.json(
        {
          error:
            "the request body is not a valid rule set: json: cannot unmarshal number into Go struct field RuleSet.version of type string",
        },
        { status: 400 },
      ),
    ),
  );
  renderWithProviders(undefined, { route: "/console/policy" });
  await userEvent.click(await screen.findByRole("button", { name: /new revision/i }));
  const editor = await screen.findByRole("textbox", { name: /policy yaml/i });
  await userEvent.clear(editor);
  await userEvent.type(editor, "version: 2026.10\n");
  await userEvent.click(screen.getByRole("button", { name: /review changes/i }));
  const dialog = await screen.findByRole("dialog");
  await userEvent.click(within(dialog).getByRole("button", { name: /apply revision/i }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/cannot unmarshal number/i);
  expect(screen.getByRole("textbox", { name: /policy yaml/i })).toHaveValue("version: 2026.10\n");
});

test("the editor says comments are not kept", async () => {
  server.use(http.get("/v1/policy/revisions", () => HttpResponse.json(revisions)));
  renderWithProviders(undefined, { route: "/console/policy" });
  await userEvent.click(await screen.findByRole("button", { name: /new revision/i }));
  expect(await screen.findByText(/comments are not kept/i)).toBeInTheDocument();
});

// Final review finding 1(b): a failed revisions query shows an alert, not
// the "No revisions yet." empty state.
test("a 403 shows an alert, not the empty-revisions message", async () => {
  server.use(http.get("/v1/policy/revisions", () => HttpResponse.json({ error: "not a console admin" }, { status: 403 })));
  renderWithProviders(undefined, { route: "/console/policy" });
  expect(await screen.findByRole("alert")).toHaveTextContent(/not a console admin/i);
  expect(screen.queryByText(/no revisions yet/i)).not.toBeInTheDocument();
});

// Final review finding 8: clicking "New revision" before the revisions
// query resolves must not seed the draft from the empty template forever —
// once the real current revision arrives, the still-untouched draft picks
// it up.
test("clicking New revision before revisions load re-seeds the draft once they arrive", async () => {
  server.use(http.get("/v1/policy/revisions", async () => {
    await delay(30);
    return HttpResponse.json(revisions);
  }));
  renderWithProviders(undefined, { route: "/console/policy" });
  await userEvent.click(await screen.findByRole("button", { name: /new revision/i }));
  const editor = await screen.findByRole("textbox", { name: /policy yaml/i });
  // Revisions haven't arrived yet: the draft starts from the empty template.
  expect((editor as HTMLTextAreaElement).value).not.toContain("v2");
  await waitFor(() => expect((editor as HTMLTextAreaElement).value).toContain("version: v2"));
});
