import { useQuery } from "@tanstack/react-query";
import { api } from "@/api";
import type { Machine } from "@/types";

// The machines list is already wired here so the auth gate's "any 401
// returns to sign-in" behavior works from this route too; the table and
// revoke UI that reads it lands in a later task.
export function Machines() {
  useQuery({ queryKey: ["machines"], queryFn: () => api.get<Machine[]>("/v1/machines") });
  return <h1 className="text-xl font-semibold">Machines</h1>;
}
