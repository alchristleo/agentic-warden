import { parseDocument, stringify } from "yaml";

export function toYaml(ruleSet: unknown): string {
  return stringify(ruleSet, { indent: 2, lineWidth: 0 });
}

export type Parsed = { ok: true; value: unknown } | { ok: false; message: string; line: number };

// parsePolicy mirrors the CLI's sigs.k8s.io/yaml UnmarshalStrict closely
// enough for authoring: duplicate keys are errors, and the result is plain
// JSON data. Type checks stay with the server.
export function parsePolicy(text: string): Parsed {
  if (text.trim() === "") return { ok: false, message: "The document is empty.", line: 1 };
  const doc = parseDocument(text, { uniqueKeys: true, prettyErrors: true });
  const err = doc.errors[0];
  if (err) {
    // Some errors (an unclosed flow sequence, for one) are only detected once
    // the parser runs off the end of the document, and get reported one line
    // past the text a person actually wrote — the blank line implied by a
    // trailing "\n". Clamp back to the last real line so the editor points at
    // the content, not at EOF.
    const lastLine = text.split("\n").length - (text.endsWith("\n") ? 1 : 0);
    const line = Math.min(err.linePos?.[0]?.line ?? 1, Math.max(lastLine, 1));
    return { ok: false, message: err.message, line };
  }
  return { ok: true, value: doc.toJS() };
}
