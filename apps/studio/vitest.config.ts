import { defineConfig } from "vitest/config";
export default defineConfig({
  test: {
    include: ["test/**/*.test.ts", "test/**/*.test.tsx"],
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts", "src/**/*.tsx"],
      exclude: ["src/client.tsx", "src/server.ts"],
      reporter: ["text-summary", "lcov"],
      thresholds: { statements: 90, branches: 80, functions: 90, lines: 90 },
    },
  },
});
