import { Suspense, lazy, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/api";
import type { Revision } from "@/types";
import { parsePolicy, toYaml } from "@/policy/yaml";
import { LineDiff } from "@/policy/diff";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

const YamlEditor = lazy(() => import("@/policy/editor"));

function RevisionRow({ revision, expanded, onToggle }: { revision: Revision; expanded: boolean; onToggle: () => void }) {
  return (
    <>
      <TableRow
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggle();
          }
        }}
        className="cursor-pointer"
      >
        <TableCell>{revision.version}</TableCell>
        <TableCell>{revision.createdBy ?? "—"}</TableCell>
        <TableCell>{revision.createdAt}</TableCell>
      </TableRow>
      {expanded && (
        <TableRow>
          <TableCell colSpan={3}>
            <pre className="overflow-x-auto rounded-md border p-3 text-sm">{toYaml(revision.ruleSet)}</pre>
          </TableCell>
        </TableRow>
      )}
    </>
  );
}

function CompareRevisions({ revisions }: { revisions: Revision[] }) {
  const [fromVersion, setFromVersion] = useState("");
  const [toVersion, setToVersion] = useState("");
  const from = revisions.find((r) => r.version === fromVersion);
  const to = revisions.find((r) => r.version === toVersion);

  return (
    <div className="flex flex-col gap-3">
      <h2 className="text-lg font-medium">Compare revisions</h2>
      <div className="flex items-end gap-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="compare-from">Compare from</Label>
          <select
            id="compare-from"
            value={fromVersion}
            onChange={(e) => setFromVersion(e.target.value)}
            className="h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm"
          >
            <option value="">Select a revision</option>
            {revisions.map((r) => (
              // The label includes the sequence number so it never collides,
              // as plain text, with the revision's own row in the table
              // above (which shows just "v2", say) — that would otherwise
              // break any `getByText` lookup for the bare version string.
              <option key={r.seq} value={r.version}>
                {r.version} (rev {r.seq})
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="compare-to">Compare to</Label>
          <select
            id="compare-to"
            value={toVersion}
            onChange={(e) => setToVersion(e.target.value)}
            className="h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm"
          >
            <option value="">Select a revision</option>
            {revisions.map((r) => (
              <option key={r.seq} value={r.version}>
                {r.version} (rev {r.seq})
              </option>
            ))}
          </select>
        </div>
      </div>
      {from && to && <LineDiff before={toYaml(from.ruleSet)} after={toYaml(to.ruleSet)} />}
    </div>
  );
}

function NewRevision({ current, onDone }: { current?: Revision; onDone: (message: string) => void }) {
  const queryClient = useQueryClient();
  const currentYaml = useMemo(() => toYaml(current?.ruleSet ?? { version: "", rules: [] }), [current]);
  const [draft, setDraft] = useState(() => currentYaml);
  const [error, setError] = useState<string | null>(null);
  const [errorLine, setErrorLine] = useState<number | undefined>(undefined);
  const [reviewValue, setReviewValue] = useState<unknown>(null);
  const [dialogOpen, setDialogOpen] = useState(false);

  const mutation = useMutation({
    mutationFn: (value: unknown) => api.post("/v1/policy/revisions", value),
    onSuccess: () => {
      setDialogOpen(false);
      const version = (reviewValue as { version?: string } | null)?.version ?? "";
      onDone(`Applied ${version}`);
      queryClient.invalidateQueries({ queryKey: ["revisions"] });
      queryClient.invalidateQueries({ queryKey: ["audit"] });
    },
    onError: (err) => {
      setDialogOpen(false);
      setError(err instanceof ApiError ? err.message : "Could not create the revision.");
    },
  });

  function review() {
    setError(null);
    const parsed = parsePolicy(draft);
    if (!parsed.ok) {
      setError(`Line ${parsed.line}: ${parsed.message}`);
      setErrorLine(parsed.line);
      return;
    }
    setErrorLine(undefined);
    setReviewValue(parsed.value);
    setDialogOpen(true);
  }

  const reviewVersion = (reviewValue as { version?: string } | null)?.version ?? "";

  return (
    <div className="flex flex-col gap-3">
      <h2 className="text-lg font-medium">New revision</h2>
      <p className="text-sm text-muted-foreground">Comments are not kept: revisions are stored as JSON.</p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Suspense fallback={<p className="text-sm text-muted-foreground">Loading editor…</p>}>
        <YamlEditor value={draft} onChange={setDraft} ariaLabel="Policy YAML" errorLine={errorLine} />
      </Suspense>
      <div>
        <Button type="button" onClick={review}>
          Review changes
        </Button>
      </div>
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Apply revision {reviewVersion}</DialogTitle>
            <DialogDescription>Review the changes before applying this revision.</DialogDescription>
          </DialogHeader>
          <LineDiff before={currentYaml} after={toYaml(reviewValue)} />
          <DialogFooter>
            <Button
              type="button"
              disabled={mutation.isPending}
              onClick={() => mutation.mutate(reviewValue)}
            >
              Apply revision
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export function Policy() {
  const { data } = useQuery({ queryKey: ["revisions"], queryFn: () => api.get<Revision[]>("/v1/policy/revisions") });
  const revisions = useMemo(() => data ?? [], [data]);
  const [expanded, setExpanded] = useState<number | null>(null);
  const [status, setStatus] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Policy</h1>
        <Button type="button" onClick={() => setCreating(true)}>
          New revision
        </Button>
      </div>
      <div role="status" className="text-sm text-muted-foreground">
        {status}
      </div>
      {revisions.length === 0 ? (
        <p className="text-sm text-muted-foreground">No revisions yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Version</TableHead>
              <TableHead>Applied by</TableHead>
              <TableHead>Applied</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {revisions.map((r) => (
              <RevisionRow
                key={r.seq}
                revision={r}
                expanded={expanded === r.seq}
                onToggle={() => setExpanded(expanded === r.seq ? null : r.seq)}
              />
            ))}
          </TableBody>
        </Table>
      )}
      {revisions.length > 1 && <CompareRevisions revisions={revisions} />}
      {creating && (
        <NewRevision
          current={revisions[0]}
          onDone={(message) => {
            setStatus(message);
            setCreating(false);
          }}
        />
      )}
    </div>
  );
}
