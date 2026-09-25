import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig({
  base: "/console/",
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "src") } },
  build: {
    outDir: "../internal/console/dist",
    emptyOutDir: true,
    // The CSP forbids inline scripts; the polyfill injects one.
    modulePreload: { polyfill: false },
  },
  server: { proxy: { "/v1": "http://127.0.0.1:8080", "/console/api": "http://127.0.0.1:8080", "/console/auth": "http://127.0.0.1:8080" } },
});
