"use client";

import { useEffect } from "react";

/** Delegated click handler for the copy buttons that the markdown renderer puts on code blocks. */
export function DocsCopy() {
  useEffect(() => {
    const onClick = async (e: MouseEvent) => {
      const button = (e.target as Element).closest<HTMLButtonElement>(".copy-button");
      const code = button?.parentElement?.querySelector("pre")?.textContent;
      if (!button || code == null) return;
      await navigator.clipboard.writeText(code.replace(/\n$/, ""));
      button.textContent = "Copied";
      setTimeout(() => (button.textContent = "Copy"), 1500);
    };
    document.addEventListener("click", onClick);
    return () => document.removeEventListener("click", onClick);
  }, []);
  return null;
}
