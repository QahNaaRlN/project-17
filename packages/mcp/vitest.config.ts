import { defineConfig } from "vitest/config";
export default defineConfig({
  test: {
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      exclude: ["src/stdio.ts"],
      thresholds: { statements: 95, branches: 90, functions: 95, lines: 95 },
      reporter: ["text-summary", "lcov"],
    },
  },
});
