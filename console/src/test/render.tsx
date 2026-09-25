import type { ReactNode } from "react";
import { render, type RenderResult } from "@testing-library/react";
import axe from "axe-core";
import { expect } from "vitest";
import { App, makeQueryClient } from "@/app";

export function renderWithProviders(
  _ui?: ReactNode,
  { route = "/console/" }: { route?: string } = {},
): RenderResult {
  window.history.pushState({}, "", route);
  // makeQueryClient() already sets retry: false and wires the 401 → ["me"]
  // invalidation the auth gate depends on; a bare `new QueryClient()` here
  // would skip that and silently break the "any 401 returns to sign-in" test.
  const client = makeQueryClient();
  return render(<App client={client} />);
}

export async function expectNoAxeViolations(container: HTMLElement) {
  const result = await axe.run(container, { rules: { "color-contrast": { enabled: false } } });
  expect(result.violations.map((v) => v.id)).toEqual([]);
}
