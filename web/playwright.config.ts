import { defineConfig, devices, type ReporterDescription } from "@playwright/test";

const allowWebKit = Boolean(process.env.CI) || process.env.DOSH_PLAYWRIGHT_ALLOW_WEBKIT === "1";
const reporters: ReporterDescription[] = [
  ["list"],
  ["html", { outputFolder: "playwright-report", open: "never" }],
];
if (process.env.GITHUB_ACTIONS) {
  reporters.splice(1, 0, ["github"]);
}

export default defineConfig({
  testDir: "./e2e",
  testIgnore: ["performance/**"],
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  workers: 1,
  reporter: reporters,
  outputDir: "test-results",
  use: {
    baseURL: "http://127.0.0.1:5173",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
    ...(allowWebKit ? [{ name: "webkit", use: { ...devices["Desktop Safari"] } }] : []),
  ],
  webServer: {
    command: "npm run dev -- --host 127.0.0.1 --port 5173",
    url: "http://127.0.0.1:5173",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
