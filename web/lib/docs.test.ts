import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { assetFiles, assetPath, docSlug, loadDocs, navSections, parseDoc, renderMarkdown } from "./docs";

const page = (fm: string, body = "Body.") => `---\n${fm}\n---\n${body}\n`;

function dirWith(files: Record<string, string>) {
  const dir = mkdtempSync(join(tmpdir(), "docs-"));
  for (const [name, text] of Object.entries(files)) writeFileSync(join(dir, name), text);
  return dir;
}

describe("parseDoc", () => {
  it("reads frontmatter and body", () => {
    const d = parseDoc(
      "quickstart.md",
      page("title: Quickstart\nsection: get-started\norder: 2\ndescription: One line: with colon.", "## Hi"),
    );
    expect(d).toMatchObject({
      slug: "quickstart",
      title: "Quickstart",
      section: "get-started",
      order: 2,
      description: "One line: with colon.",
      body: "## Hi\n",
    });
  });
  it("drops a leading h1 because the page renders the title", () => {
    expect(parseDoc("a.md", page("title: A\nsection: guides\norder: 1\ndescription: d", "# A\n\ntext")).body).toBe("text\n");
  });
  it("keeps a # inside a value, strips one pair of quotes, and reads CRLF", () => {
    const raw = '---\r\ntitle: "Fix issue #12"\r\nsection: guides\r\norder: 3\r\ndescription: \'Use a # here\'\r\n---\r\ntext\r\n';
    expect(parseDoc("a.md", raw)).toMatchObject({ title: "Fix issue #12", description: "Use a # here", order: 3 });
    expect(parseDoc("a.md", page("title: A #1 thing\nsection: guides\norder: 1\ndescription: \"it's\"")).title).toBe("A #1 thing");
  });
  it("renders extra body h1s as h2", () => {
    expect(renderMarkdown("# Again\n\n## Two\n")).toBe('<h2>Again</h2>\n<h2>Two</h2>\n');
  });
  it("drops a leading h1 even when its text differs from the title", () => {
    expect(parseDoc("a.md", page("title: A\nsection: guides\norder: 1\ndescription: d", "# Other words\n\ntext")).body).toBe("text\n");
  });
  it("rewrites GitHub-style links to site routes and leaves others alone", () => {
    const html = renderMarkdown(
      "[a](./quickstart.md) [b](./index.md#top) [c](../assets/x.gif) ![d](../assets/hero.gif) [e](https://x.test/y.md) [f](/docs/z) [g](#h)",
    );
    expect(html).toContain('href="/docs/quickstart"');
    expect(html).toContain('href="/docs#top"');
    expect(html).toContain('href="/docs-assets/x.gif"');
    expect(html).toContain('src="/docs-assets/hero.gif"');
    expect(html).toContain('href="https://x.test/y.md"');
    expect(html).toContain('href="/docs/z"');
    expect(html).toContain('href="#h"');
  });
  it("rejects an unknown section", () => {
    expect(() => parseDoc("a.md", page("title: A\nsection: blog\norder: 1\ndescription: d"))).toThrow(/unknown section "blog"/);
  });
  it("rejects missing fields and a non-numeric order", () => {
    expect(() => parseDoc("a.md", page("title: A\nsection: guides\norder: 1"))).toThrow(/description/);
    expect(() => parseDoc("a.md", page("title: A\nsection: guides\norder: x\ndescription: d"))).toThrow(/order/);
    expect(() => parseDoc("a.md", "no frontmatter")).toThrow(/frontmatter/);
  });
});

describe("docSlug", () => {
  it("maps index to the empty slug", () => {
    expect(docSlug("index")).toEqual([]);
    expect(docSlug("quickstart")).toEqual(["quickstart"]);
  });
});

describe("loadDocs and navSections", () => {
  const dir = dirWith({
    "ref.md": page("title: Env\nsection: reference\norder: 1\ndescription: d"),
    "b.md": page("title: B\nsection: get-started\norder: 2\ndescription: d"),
    "index.md": page("title: Overview\nsection: get-started\norder: 1\ndescription: d"),
    "sec.md": page("title: Sec\nsection: security-review\norder: 1\ndescription: d"),
    "notes.txt": "ignored",
  });
  it("loads only markdown files", () => {
    expect(loadDocs(dir).map((d) => d.slug).sort()).toEqual(["b", "index", "ref", "sec"]);
  });
  it("orders sections Get started, Security review, Guides, Reference and pages by order; skips empty sections", () => {
    expect(navSections(loadDocs(dir)).map((s) => [s.label, s.pages.map((p) => p.title)])).toEqual([
      ["Get started", ["Overview", "B"]],
      ["Security review", ["Sec"]],
      ["Reference", ["Env"]],
    ]);
  });
});

describe("assetPath", () => {
  it("accepts a plain image name and rejects traversal and other types", () => {
    expect(assetPath("hero.gif")).toMatch(/docs[\\/]assets[\\/]hero\.gif$/);
    expect(assetPath("../site/index.md")).toBeNull();
    expect(assetPath("a/../../x.png")).toBeNull();
    expect(assetPath("notes.md")).toBeNull();
    expect(assetPath("../x.png")).toBeNull();
    expect(assetPath(".hidden.png")).toBeNull();
    expect(assetPath("x.png")).toBeNull();
  });

  it("lists the servable images", () => {
    expect(assetFiles()).toContain("logo.svg");
  });
});

describe("renderMarkdown escaping", () => {
  it("escapes double quotes in the fence language", () => {
    expect(renderMarkdown('```a"b\nx\n```')).toContain('class="language-a&quot;b"');
  });
});
