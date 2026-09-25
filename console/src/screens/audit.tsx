import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { api } from "@/api";
import type { AuditEvent } from "@/types";
import { relative } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

const PAGE_SIZE = 50;

async function fetchAuditPage(before?: number): Promise<AuditEvent[]> {
  const params = new URLSearchParams({ limit: String(PAGE_SIZE) });
  if (before !== undefined) params.set("before", String(before));
  return api.get<AuditEvent[]>(`/v1/audit?${params.toString()}`);
}

export function Audit() {
  const [actor, setActor] = useState("");
  const [action, setAction] = useState("All");

  const query = useInfiniteQuery({
    queryKey: ["audit"],
    queryFn: ({ pageParam }) => fetchAuditPage(pageParam),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) => (lastPage.length === PAGE_SIZE ? lastPage[lastPage.length - 1].id : undefined),
  });

  const events = useMemo(() => query.data?.pages.flat() ?? [], [query.data]);

  const actions = useMemo(() => {
    const distinct = new Set(events.map((e) => e.action));
    return ["All", ...[...distinct].sort()];
  }, [events]);

  const filtered = events.filter(
    (e) =>
      (actor === "" || e.actor.toLowerCase().includes(actor.toLowerCase())) &&
      (action === "All" || e.action === action),
  );

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Audit</h1>
      <div className="flex items-end gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="audit-actor">Actor</Label>
          <Input id="audit-actor" value={actor} onChange={(e) => setActor(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="audit-action">Action</Label>
          <select
            id="audit-action"
            value={action}
            onChange={(e) => setAction(e.target.value)}
            className="h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm"
          >
            {actions.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </div>
      </div>
      {filtered.length === 0 ? (
        <p className="text-sm text-muted-foreground">No audit events</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Time</TableHead>
              <TableHead>Actor</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Target</TableHead>
              <TableHead>Detail</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((e) => (
              <TableRow key={e.id}>
                <TableCell>
                  {new Date(e.at).toLocaleString()} ({relative(e.at)})
                </TableCell>
                <TableCell>{e.actor}</TableCell>
                <TableCell>{e.action}</TableCell>
                <TableCell>{e.target ?? "—"}</TableCell>
                <TableCell>
                  <code className="text-xs">{e.detail ? JSON.stringify(e.detail) : "—"}</code>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {query.hasNextPage && (
        <Button variant="outline" onClick={() => query.fetchNextPage()} disabled={query.isFetchingNextPage}>
          Older
        </Button>
      )}
    </div>
  );
}
