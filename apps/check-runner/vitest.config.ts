import { defineConfig } from "vitest/config";
export default defineConfig({
  test: {
    testTimeout: 60_000,
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      exclude: ["src/main.ts"],
      thresholds: { statements: 90, branches: 80, functions: 90, lines: 90 },
      reporter: ["text-summary", "lcov"],
    },
  },
});
