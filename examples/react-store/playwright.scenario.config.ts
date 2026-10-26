import { defineConfig } from "@playwright/test";

// STU-440/456/457: the §2 scenario against the real CMS stand. One worker, no
// retries of checks; the stand retries only connections while it starts.
export default defineConfig({
  testDir: "e2e/scenario",
  workers: 1,
  retries: 0,
  timeout: 20 * 60_000,
  expect: { timeout: 15_000 },
  outputDir: "test-results/scenario",
  reporter: [["list"], ["json", { outputFile: "test-results/scenario-report.json" }]],
  use: {
    browserName: "chromium",
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    launchOptions: process.env.PLAYWRIGHT_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
      : {},
  },
});
