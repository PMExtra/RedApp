import { defineConfig, devices } from "@playwright/test";

/**
 * Smoke tests against a real RedApp server with the embedded frontend
 * (`make build`, then run `bin/redapp`). Configure with:
 * - REDAPP_E2E_URL: server origin (default http://127.0.0.1:8080)
 * - REDAPP_E2E_PASSWORD: administrator password (from the first-start log)
 */
export default defineConfig({
  testDir: "e2e",
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: process.env.REDAPP_E2E_URL ?? "http://127.0.0.1:8080",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
