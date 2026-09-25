import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": path.resolve(__dirname, "src") } },
  test: {
    environment: "jsdom",
    setupFiles: ["src/test/setup.ts"],
    // e2e/ holds Playwright specs (console.spec.ts), which vitest's default
    // include glob would otherwise also pick up and fail to run.
    exclude: ["**/node_modules/**", "**/dist/**", "e2e/**"],
  },
});
