import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.DOSH_E2E_BASE_URL ?? "https://192.168.122.100:32629";
const allowWebKit = Boolean(process.env.CI) || process.env.DOSH_PLAYWRIGHT_ALLOW_WEBKIT === "1";

export default defineConfig({
  testDir: "./e2e-local",
  fullyParallel: false,
  forbidOnly: true,
  retries: 0,
  workers: 1,
  reporter: [["list"]],
  outputDir: "test-results-local",
  use: {
    baseURL,
    ignoreHTTPSErrors: true,
    trace: "off",
    screenshot: "off",
    video: "off",
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
    ...(allowWebKit ? [{ name: "webkit", use: { ...devices["Desktop Safari"] } }] : []),
  ],
});
