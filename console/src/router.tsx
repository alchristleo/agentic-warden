import {
  Link,
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  useRouteContext,
} from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "@/api";
import type { Me } from "@/types";
import { currentTheme, setStoredTheme, type Theme } from "@/theme";
import { Overview } from "@/screens/overview";
import { Policy } from "@/screens/policy";
import { Machines } from "@/screens/machines";
import { Enrollment } from "@/screens/enrollment";
import { Groups } from "@/screens/groups";
import { Audit } from "@/screens/audit";

export interface RouterContext {
  me: Me;
}

const navItems = [
  { to: "/", label: "Overview" },
  { to: "/policy", label: "Policy" },
  { to: "/machines", label: "Machines" },
  { to: "/enrollment", label: "Enrollment" },
  { to: "/groups", label: "Groups" },
  { to: "/audit", label: "Audit" },
] as const;

function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() => currentTheme());
  function toggle() {
    const next: Theme = theme === "dark" ? "light" : "dark";
    setStoredTheme(next);
    setTheme(next);
  }
  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
      className="rounded-md border border-border px-2 py-1 text-sm hover:bg-muted"
    >
      {theme === "dark" ? "Dark" : "Light"}
    </button>
  );
}

function RootLayout() {
  const { me } = useRouteContext({ from: "__root__" });
  const queryClient = useQueryClient();
  const [signingOut, setSigningOut] = useState(false);

  async function signOut() {
    setSigningOut(true);
    try {
      await api.post("/console/auth/logout", undefined);
    } finally {
      setSigningOut(false);
      await queryClient.invalidateQueries({ queryKey: ["me"] });
    }
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="flex items-center justify-between border-b border-border px-6 py-4">
        <span className="text-lg font-semibold">Agentic Warden console</span>
        <div className="flex items-center gap-4">
          <span className="text-sm text-muted-foreground">{me.user}</span>
          <ThemeToggle />
          <button
            type="button"
            onClick={signOut}
            disabled={signingOut}
            className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-muted disabled:opacity-50"
          >
            Sign out
          </button>
        </div>
      </header>
      <nav aria-label="Console" className="flex gap-4 border-b border-border px-6 py-2">
        {navItems.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            className="text-sm font-medium text-muted-foreground hover:text-foreground [&.active]:text-foreground"
            activeOptions={{ exact: item.to === "/" }}
          >
            {item.label}
          </Link>
        ))}
      </nav>
      <main className="p-6">
        <Outlet />
      </main>
    </div>
  );
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
});

const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: Overview });
const policyRoute = createRoute({ getParentRoute: () => rootRoute, path: "/policy", component: Policy });
const machinesRoute = createRoute({ getParentRoute: () => rootRoute, path: "/machines", component: Machines });
const enrollmentRoute = createRoute({ getParentRoute: () => rootRoute, path: "/enrollment", component: Enrollment });
const groupsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/groups", component: Groups });
const auditRoute = createRoute({ getParentRoute: () => rootRoute, path: "/audit", component: Audit });

const routeTree = rootRoute.addChildren([
  indexRoute,
  policyRoute,
  machinesRoute,
  enrollmentRoute,
  groupsRoute,
  auditRoute,
]);

export function buildRouter() {
  return createRouter({
    routeTree,
    basepath: "/console",
    // Real context is supplied by <RouterProvider context={{ me }} />, which
    // updates the router before the first match renders; this placeholder
    // only satisfies the type since createRouter requires one up front.
    context: { me: undefined as unknown as Me },
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof buildRouter>;
  }
}
