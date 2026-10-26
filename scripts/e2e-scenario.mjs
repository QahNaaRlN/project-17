// STU-459: one command for the §2 scenario: `pnpm e2e:scenario`.
// Builds the CMS with the e2e tag, Studio and the store, then runs the scenario.
// CMS_DATABASE_URL must point to an empty loopback PostgreSQL database.
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { repo } from "./e2e-stand.mjs";

const directory = resolve(
  process.env.E2E_DIR ?? join(repo, "examples/react-store/test-results/scenario-stand"),
);
mkdirSync(directory, { recursive: true });
const binary = join(directory, process.platform === "win32" ? "cms.exe" : "cms");
const run = (command, args, cwd = repo) =>
  execFileSync(command, args, {
    cwd,
    stdio: "inherit",
    windowsHide: true,
    shell: process.platform === "win32",
  });
if (process.env.E2E_SKIP_BUILD !== "true") {
  run("go", ["build", "-tags", "e2e", "-o", binary, "./cmd/cms"], join(repo, "apps/server"));
  run("pnpm", ["--filter", "@cms/studio", "build"]);
  run("pnpm", ["--filter", "@cms/store-example", "build"]);
}
process.env.E2E_DIR = directory;
process.env.E2E_CMS_BINARY = binary;
run("pnpm", [
  "--filter",
  "@cms/store-example",
  "exec",
  "playwright",
  "test",
  "-c",
  "playwright.scenario.config.ts",
]);
