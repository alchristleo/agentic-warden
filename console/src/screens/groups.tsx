import { useQuery } from "@tanstack/react-query";
import { api } from "@/api";
import type { GroupsView } from "@/types";

// The groups view is already wired here so the auth gate's "any 401 returns
// to sign-in" behavior works from this route too; the snapshot and
// resolution UI that reads it lands in a later task.
export function Groups() {
  useQuery({ queryKey: ["groups"], queryFn: () => api.get<GroupsView>("/v1/groups") });
  return <h1 className="text-xl font-semibold">Groups</h1>;
}
