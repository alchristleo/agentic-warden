import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api, ApiError } from "@/api";
import type { GroupsView, GroupSources } from "@/types";
import { relative } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

// Exported so the Overview screen can reuse the ["groups"] query without
// duplicating the "404 means no snapshot yet" mapping.
export async function fetchGroups(): Promise<GroupsView | null> {
  try {
    return await api.get<GroupsView>("/v1/groups");
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}

function groupCounts(members: Record<string, string[]> | undefined): [string, number][] {
  const counts = new Map<string, number>();
  for (const groups of Object.values(members ?? {})) {
    for (const group of groups) counts.set(group, (counts.get(group) ?? 0) + 1);
  }
  return [...counts.entries()].sort(([a], [b]) => a.localeCompare(b));
}

function ResolveForm() {
  const [input, setInput] = useState("");
  const [user, setUser] = useState("");
  const query = useQuery({
    queryKey: ["resolve", user],
    queryFn: () => api.get<GroupSources>(`/v1/groups/resolve?user=${encodeURIComponent(user)}`),
    enabled: Boolean(user),
  });

  return (
    <div className="flex flex-col gap-3">
      <h2 className="text-lg font-medium">Resolve a user's groups</h2>
      <div className="flex items-end gap-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="resolve-user">User</Label>
          <Input id="resolve-user" value={input} onChange={(e) => setInput(e.target.value)} />
        </div>
        <Button type="button" onClick={() => setUser(input)} disabled={!input}>
          Resolve
        </Button>
      </div>
      {query.isError && (
        <p role="alert" className="text-sm text-destructive">
          {query.error instanceof ApiError ? query.error.message : "Could not resolve groups."}
        </p>
      )}
      {query.data && (
        <div className="flex flex-col gap-2">
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
            <dt className="font-medium">Authored</dt>
            <dd>{query.data.authored.join(", ") || "—"}</dd>
            <dt className="font-medium">Snapshot</dt>
            <dd>{query.data.snapshot.join(", ") || "—"}</dd>
            <dt className="font-medium">SCIM</dt>
            <dd>{query.data.scim.join(", ") || "—"}</dd>
            <dt className="font-medium">Effective</dt>
            <dd>{query.data.effective.join(", ") || "—"}</dd>
          </dl>
          {query.data.scimNearMatch && (
            <p role="status" className="text-sm text-amber-600">
              SCIM has {query.data.scimNearMatch} — differs only in case; fix the IdP attribute mapping.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

export function Groups() {
  const { data: groups, isPending } = useQuery({ queryKey: ["groups"], queryFn: fetchGroups });

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-xl font-semibold">Groups</h1>
      {isPending ? null : !groups ? (
        <p className="text-sm text-muted-foreground">
          No group data yet. Sync SCIM or run <code>awd groups apply</code> to load one.
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1 text-sm">
            <p>
              Source: {groups.source ?? "—"} · Applied by: {groups.appliedBy ?? "—"} ·{" "}
              {groups.syncedAt ? `Synced ${relative(groups.syncedAt)}` : "Never synced"}
            </p>
            {groups.scim && (
              <p>
                SCIM: {groups.scim.users} users ({groups.scim.activeUsers} active), {groups.scim.groups} group
                {groups.scim.groups === 1 ? "" : "s"}
              </p>
            )}
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Group</TableHead>
                <TableHead>Members</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {groupCounts(groups.members).map(([group, count]) => (
                <TableRow key={group}>
                  <TableCell>{group}</TableCell>
                  <TableCell>{count}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <ResolveForm />
    </div>
  );
}
