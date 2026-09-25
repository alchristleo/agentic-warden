// Dark mode is class-based (.dark on <html>), so the toggle has to run
// before React renders to avoid a flash of the wrong theme. It cannot be an
// inline <script> in index.html: the CSP is script-src 'self'.
const STORAGE_KEY = "aw-theme";

export type Theme = "light" | "dark";

function readStored(): Theme | null {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return stored === "light" || stored === "dark" ? stored : null;
  } catch {
    return null;
  }
}

function preferredTheme(): Theme {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function currentTheme(): Theme {
  return readStored() ?? preferredTheme();
}

export function applyTheme(theme: Theme) {
  document.documentElement.classList.toggle("dark", theme === "dark");
}

export function setStoredTheme(theme: Theme) {
  applyTheme(theme);
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    // Best effort: the theme still applies for this load.
  }
}

export function applyStoredTheme() {
  applyTheme(currentTheme());
}
