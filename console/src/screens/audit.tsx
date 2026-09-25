import { useQuery } from "@tanstack/react-query";
import { api } from "@/api";
import type { AuditEvent } from "@/types";

// The audit log is already wired here so the auth gate's "any 401 returns
// to sign-in" behavior works from this route too; the table UI that reads
// it lands in a later task.
export function Audit() {
  useQuery({ queryKey: ["audit"], queryFn: () => api.get<AuditEvent[]>("/v1/audit") });
  return <h1 className="text-xl font-semibold">Audit</h1>;
}
