import { diffLines } from "diff";

export function LineDiff({ before, after }: { before: string; after: string }) {
  const parts = diffLines(before, after);
  return (
    <pre className="overflow-x-auto rounded-md border p-3 text-sm" aria-label="Changes">
      {parts.flatMap((p, i) =>
        p.value
          .replace(/\n$/, "")
          .split("\n")
          .map((line, j) => (
            <div
              key={`${i}-${j}`}
              className={p.added ? "bg-green-500/10" : p.removed ? "bg-red-500/10" : "text-muted-foreground"}
            >
              {(p.added ? "+ " : p.removed ? "- " : "  ") + line}
            </div>
          )),
      )}
    </pre>
  );
}
