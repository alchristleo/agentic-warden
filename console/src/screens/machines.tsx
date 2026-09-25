import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { useState } from "react";
import { api, ApiError } from "@/api";
import type { Machine } from "@/types";
import { isOldKey, isStale, neverSeen, relative } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

function RevokeButton({ machine, onResolved }: { machine: Machine; onResolved: (message: string) => void }) {
  const [open, setOpen] = useState(false);
  const [confirmText, setConfirmText] = useState("");
  const queryClient = useQueryClient();
  const label = machine.name || machine.id;
  const inputId = `revoke-confirm-${machine.id}`;

  function invalidate() {
    queryClient.invalidateQueries({ queryKey: ["machines"] });
    queryClient.invalidateQueries({ queryKey: ["audit"] });
  }

  const mutation = useMutation({
    mutationFn: () => api.del(`/v1/machines/${encodeURIComponent(machine.id)}`),
    onSuccess: () => {
      setOpen(false);
      onResolved(`Revoked ${label}`);
      invalidate();
    },
    onError: (error) => {
      // A 404 means someone else already revoked this machine: treat it the
      // same as success (close, tell the operator, refresh the list). Any
      // other error leaves the dialog open with an inline alert instead, and
      // does not invalidate: nothing on the server actually changed.
      if (error instanceof ApiError && error.status === 404) {
        setOpen(false);
        onResolved(`${label} was already revoked`);
        invalidate();
      }
    },
  });

  const alertMessage =
    mutation.isError && !(mutation.error instanceof ApiError && mutation.error.status === 404)
      ? (mutation.error as Error).message
      : null;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setConfirmText("");
      }}
    >
      <DialogTrigger
        render={
          <Button variant="destructive" size="sm" aria-label={`Revoke ${label}`}>
            Revoke
          </Button>
        }
      />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Type {label} to confirm</DialogTitle>
          <DialogDescription>
            This revokes {label}&rsquo;s credential. It can re-enroll with a new token.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={inputId}>Type {label} to confirm</Label>
          <Input id={inputId} value={confirmText} onChange={(e) => setConfirmText(e.target.value)} />
        </div>
        {alertMessage && (
          <p role="alert" className="text-sm text-destructive">
            {alertMessage}
          </p>
        )}
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={confirmText !== label || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            Revoke machine
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function Machines() {
  const { me } = useRouteContext({ from: "__root__" });
  const { data } = useQuery({ queryKey: ["machines"], queryFn: () => api.get<Machine[]>("/v1/machines") });
  const [status, setStatus] = useState<string | null>(null);
  const machines = data ?? [];

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Machines</h1>
      <div role="status" className="text-sm text-muted-foreground">
        {status}
      </div>
      {machines.length === 0 ? (
        <p className="text-sm text-muted-foreground">No machines enrolled yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>OS</TableHead>
              <TableHead>Last seen</TableHead>
              <TableHead>Bundle</TableHead>
              <TableHead>Key</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {machines.map((m) => (
              <TableRow key={m.id}>
                <TableCell>{m.user}</TableCell>
                <TableCell>{m.name || "—"}</TableCell>
                <TableCell>{m.os || "—"}</TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <span>{neverSeen(m) ? "Never" : relative(m.lastSeenAt)}</span>
                    {neverSeen(m) ? (
                      <Badge variant="secondary">Never seen</Badge>
                    ) : isStale(m) ? (
                      <Badge variant="secondary">Stale</Badge>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell>{m.lastBundleVersion || "—"}</TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <span>{m.lastKeyId || "—"}</span>
                    {isOldKey(m, me.signingKeyId) && <Badge variant="destructive">Old key</Badge>}
                  </div>
                </TableCell>
                <TableCell>
                  <RevokeButton machine={m} onResolved={setStatus} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
