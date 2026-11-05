import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e/performance",
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  workers: 1,
  reporter: [["list"], ["html", { outputFolder: "playwright-report/performance", open: "never" }]],
  outputDir: "test-results/performance",
  use: {
    baseURL: "http://127.0.0.1:4173",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
  },
  projects: [{ name: "chromium-production", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "npm run build && npm run preview -- --host 127.0.0.1 --port 4173",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: false,
    timeout: 180_000,
  },
});
