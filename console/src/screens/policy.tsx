import { useQuery } from "@tanstack/react-query";
import { api } from "@/api";
import type { Revision } from "@/types";

// The revisions list is already wired here so the auth gate's "any 401
// returns to sign-in" behavior works from this route too; the editor and
// history UI that reads it lands in a later task.
export function Policy() {
  useQuery({ queryKey: ["policy-revisions"], queryFn: () => api.get<Revision[]>("/v1/policy/revisions") });
  return <h1 className="text-xl font-semibold">Policy</h1>;
}
