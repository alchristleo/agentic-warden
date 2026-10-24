import { readdirSync, readFileSync } from "node:fs";
import { basename, join } from "node:path";
import { Marked } from "marked";

// Next runs from web/, so the repo docs are one level up.
const DOCS_DIR = join(process.cwd(), "..", "docs", "site");
const ASSETS_DIR = join(process.cwd(), "..", "docs", "assets");

export const SECTIONS = [
  { id: "get-started", label: "Get started" },
  { id: "security-review", label: "Security review" },
  { id: "guides", label: "Guides" },
  { id: "reference", label: "Reference" },
] as const;

export type SectionId = (typeof SECTIONS)[number]["id"];

export type Doc = {
  slug: string;
  title: string;
  section: SectionId;
  order: number;
  description: string;
  body: string;
};

export function parseDoc(file: string, raw: string): Doc {
  const m = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/.exec(raw);
  if (!m) throw new Error(`${file}: missing frontmatter`);
  const fm: Record<string, string> = {};
  for (const line of m[1].split(/\r?\n/)) {
    const kv = /^(\w+):\s*(.*?)\s*$/.exec(line);
    if (kv) fm[kv[1]] = kv[2].replace(/^(["'])(.*)\1$/, "$2");
  }
  for (const key of ["title", "section", "order", "description"]) {
    if (!fm[key]) throw new Error(`${file}: frontmatter needs ${key}`);
  }
  const section = SECTIONS.find((s) => s.id === fm.section);
  if (!section) throw new Error(`${file}: unknown section "${fm.section}"`);
  if (!/^\d+$/.test(fm.order)) throw new Error(`${file}: order must be a number`);
  return {
    slug: file.replace(/\.md$/, ""),
    title: fm.title,
    section: section.id,
    order: Number(fm.order),
    description: fm.description,
    // The page renders the title as its h1, so a leading "# Title" (kept for GitHub) is dropped.
    body: m[2].replace(/^\s*# .*\r?\n\s*/, ""),
  };
}

export function loadDocs(dir: string = DOCS_DIR): Doc[] {
  return readdirSync(dir)
    .filter((f) => f.endsWith(".md"))
    .map((f) => parseDoc(f, readFileSync(join(dir, f), "utf8")));
}

export const docSlug = (slug: string): string[] => (slug === "index" ? [] : [slug]);

export const docHref = (slug: string): string => ["/docs", ...docSlug(slug)].join("/");

export function navSections(docs: Doc[]) {
  return SECTIONS.map((s) => ({
    ...s,
    pages: docs.filter((d) => d.section === s.id).sort((a, b) => a.order - b.order),
  })).filter((s) => s.pages.length > 0);
}

const ASSET_TYPES: Record<string, string> = {
  gif: "image/gif",
  svg: "image/svg+xml",
};

export const assetType = (file: string): string | undefined => ASSET_TYPES[file.split(".").pop() ?? ""];

/** Absolute path of an image in docs/assets, or null for anything that is not a plain image file name. */
export function assetPath(file: string): string | null {
  if (basename(file) !== file || file.startsWith(".") || !assetType(file)) return null;
  return join(ASSETS_DIR, file);
}

/** File names of every servable image in docs/assets. */
export const assetFiles = (): string[] => readdirSync(ASSETS_DIR).filter((f) => assetPath(f) !== null);

const escapeHtml = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

/** Pages link the GitHub way (./slug.md, ../assets/x.gif); the site serves them at /docs/slug and /docs-assets/x.gif. */
function rewriteHref(href: string): string {
  const page = /^\.\/([\w-]+)\.md(#.*)?$/.exec(href);
  if (page) return `${docHref(page[1])}${page[2] ?? ""}`;
  const asset = /^\.\.\/assets\/([^/#?]+)$/.exec(href);
  if (asset) return `/docs-assets/${asset[1]}`;
  return href;
}

const marked = new Marked({
  walkTokens(token) {
    if (token.type === "link" || token.type === "image") token.href = rewriteHref(token.href);
    // The page title is the h1.
    if (token.type === "heading" && token.depth === 1) token.depth = 2;
  },
  renderer: {
    code({ text, lang }) {
      const cls = lang ? ` class="language-${escapeHtml(lang)}"` : "";
      return `<div class="code-block"><button type="button" class="copy-button" aria-label="Copy">Copy</button><pre><code${cls}>${escapeHtml(text)}</code></pre></div>\n`;
    },
  },
});

export function renderMarkdown(body: string): string {
  return marked.parse(body, { async: false });
}
