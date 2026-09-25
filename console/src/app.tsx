import { QueryCache, QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";
import { api, ApiError } from "@/api";
import type { Me } from "@/types";
import { buildRouter } from "@/router";
import { SignIn } from "@/screens/sign-in";

export function makeQueryClient() {
  const client: QueryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: true } },
    // Any 401 means the session is gone: re-ask /me, which flips the gate.
    // The ["me"] query's own 401 is excluded: Gate already reacts to
    // me.error directly, and invalidating it here too would have that
    // query's error handler invalidate itself again on every refetch, an
    // unbounded loop whenever the session is (still) signed out.
    queryCache: new QueryCache({
      onError: (err, query) => {
        if (err instanceof ApiError && err.status === 401 && query.queryKey[0] !== "me") {
          client.invalidateQueries({ queryKey: ["me"] });
        }
      },
    }),
  });
  return client;
}

function Message({ title, children }: { title: string; children: ReactNode }) {
  return (
    <main className="mx-auto max-w-md p-8 text-center">
      <h1 className="text-xl font-semibold">{title}</h1>
      <div className="mt-4 text-muted-foreground">{children}</div>
    </main>
  );
}

function Gate() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<Me>("/console/api/me") });
  const [router] = useState(() => buildRouter());
  if (me.isPending) return <Message title="Loading">Checking your session…</Message>;
  if (me.error instanceof ApiError) {
    if (me.error.status === 401) return <SignIn />;
    if (me.error.status === 403) return <Message title="No access">You are not a console admin.</Message>;
    return <Message title="Console unavailable">{me.error.message}</Message>;
  }
  if (me.isError) return <Message title="Console unavailable">Could not reach awd.</Message>;
  return <RouterProvider router={router} context={{ me: me.data }} />;
}

export function App({ client }: { client?: QueryClient }) {
  const [qc] = useState(() => client ?? makeQueryClient());
  return (
    <QueryClientProvider client={qc}>
      <Gate />
    </QueryClientProvider>
  );
}
