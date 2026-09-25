import { defineConfig, devices } from "@playwright/test";

const AWD = "http://127.0.0.1:9401";

export default defineConfig({
  testDir: "e2e",
  globalSetup: "./e2e/global-setup.ts",
  use: { baseURL: AWD, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: "go run ../internal/console/oidctest/cmd/fakeidp -addr 127.0.0.1:9400",
      url: "http://127.0.0.1:9400/.well-known/openid-configuration",
      reuseExistingServer: !process.env.CI,
    },
    {
      command: "go run ../cmd/awd serve",
      url: `${AWD}/healthz`,
      reuseExistingServer: !process.env.CI,
      env: {
        AWD_ADDR: "127.0.0.1:9401",
        AWD_ADMIN_TOKEN: "e2e-admin",
        AWD_PUBLIC_URL: AWD,
        AWD_CONSOLE_ISSUER: "http://127.0.0.1:9400",
        AWD_CONSOLE_CLIENT_ID: "console",
        AWD_CONSOLE_CLIENT_SECRET_FILE: "e2e/secret.txt",
        AWD_CONSOLE_ADMIN_GROUP: "console-admins",
      },
    },
  ],
});
