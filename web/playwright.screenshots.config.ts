import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.DOSH_E2E_BASE_URL ?? "https://192.168.122.100:32626";

export default defineConfig({
  testDir: "./e2e-screenshots",
  fullyParallel: false,
  forbidOnly: true,
  retries: 0,
  workers: 1,
  reporter: [["list"]],
  outputDir: "test-results-screenshots",
  use: {
    baseURL,
    ignoreHTTPSErrors: true,
    colorScheme: "light",
    locale: "zh-CN",
    viewport: { width: 1440, height: 960 },
    trace: "off",
    screenshot: "off",
    video: "off",
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 960 },
        deviceScaleFactor: 1,
      },
    },
  ],
});
