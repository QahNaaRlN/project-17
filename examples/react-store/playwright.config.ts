import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "e2e",
  // The §2 scenario runs against the real stand: playwright.scenario.config.ts.
  testIgnore: "scenario/**",
  workers: 1,
  timeout: 30_000,
  use: {
    baseURL: "http://127.0.0.1:18083",
    browserName: "chromium",
    launchOptions: process.env.PLAYWRIGHT_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
      : {},
    trace: "retain-on-failure",
  },
  webServer: {
    command: "node --import tsx e2e/serve.ts",
    url: "http://127.0.0.1:18083",
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
