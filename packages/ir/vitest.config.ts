import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["test/**/*.test.ts"],
    coverage: {
      provider: "v8",
      include: ["src/**"],
      exclude: ["src/generated/**", "src/index.ts"],
      reporter: ["text-summary", "lcov"],
      // Пороги — docs/testing.md §4. Понижать можно только с обоснованием в PR.
      thresholds: { statements: 95, branches: 85, functions: 95, lines: 95 },
    },
  },
});
