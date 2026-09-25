import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { api } from "@/api";
import type { AuditEvent, Machine, Revision } from "@/types";
import { fetchGroups } from "@/screens/groups";
import { isOldKey, relative } from "@/lib/format";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

// CardError renders a query's failure instead of that card's own empty
// state: a 403/503/500 is not the same thing as "nothing here yet".
function CardError({ query }: { query: UseQueryResult<unknown, unknown> }) {
  return (
    <p role="alert" className="text-sm text-destructive">
      {query.error instanceof Error ? query.error.message : "Could not load this."}
    </p>
  );
}

export function Overview() {
  const { me } = useRouteContext({ from: "__root__" });
  const revisions = useQuery({
    queryKey: ["revisions"],
    queryFn: () => api.get<Revision[]>("/v1/policy/revisions"),
  });
  const machines = useQuery({ queryKey: ["machines"], queryFn: () => api.get<Machine[]>("/v1/machines") });
  const groups = useQuery({ queryKey: ["groups"], queryFn: fetchGroups });
  const audit = useQuery({
    queryKey: ["audit", "recent"],
    queryFn: () => api.get<AuditEvent[]>("/v1/audit?limit=5"),
  });

  const current = revisions.data?.[0];
  const oldKeyCount = (machines.data ?? []).filter((m) => isOldKey(m, me.signingKeyId)).length;

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Overview</h1>
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Policy</CardTitle>
          </CardHeader>
          <CardContent>
            {revisions.isError ? (
              <CardError query={revisions} />
            ) : current ? (
              <p>
                {current.version} by {current.createdBy ?? "—"}
              </p>
            ) : revisions.isSuccess ? (
              <p className="text-muted-foreground">No policy yet</p>
            ) : null}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Machines</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-1">
            {machines.isError ? (
              <CardError query={machines} />
            ) : (
              machines.isSuccess && (
                <>
                  <p>{machines.data?.length ?? 0} machines</p>
                  {oldKeyCount > 0 && (
                    <p>
                      {oldKeyCount} machine{oldKeyCount === 1 ? "" : "s"} on an old key
                    </p>
                  )}
                </>
              )
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Groups</CardTitle>
          </CardHeader>
          <CardContent>
            {groups.isError ? (
              <CardError query={groups} />
            ) : groups.data ? (
              <p>
                {groups.data.source ?? "—"} ·{" "}
                {groups.data.syncedAt ? `Synced ${relative(groups.data.syncedAt)}` : "Never synced"}
              </p>
            ) : groups.isSuccess ? (
              <p className="text-muted-foreground">No group data yet</p>
            ) : null}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Recent activity</CardTitle>
          </CardHeader>
          <CardContent>
            {audit.isError ? (
              <CardError query={audit} />
            ) : audit.data && audit.data.length > 0 ? (
              <ul className="flex flex-col gap-1 text-sm">
                {audit.data.map((e) => (
                  <li key={e.id}>
                    {e.actor} {e.action} {e.target ?? ""} · {relative(e.at)}
                  </li>
                ))}
              </ul>
            ) : audit.isSuccess ? (
              <p className="text-muted-foreground">No recent activity</p>
            ) : null}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
