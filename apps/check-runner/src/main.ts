#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { chromium } from "playwright";
import { createClient, step } from "./runner.js";

// STU-492: CMS_API_URL, the cms-checks token (CMS_CHECK_RUNNER_TOKEN or a file),
// CMS_PROJECT and CHECK_RUNNER_CONCURRENCY (1…4).
const env = process.env;
const token =
  env.CMS_CHECK_RUNNER_TOKEN ??
  (env.CMS_CHECK_RUNNER_TOKEN_FILE
    ? readFileSync(env.CMS_CHECK_RUNNER_TOKEN_FILE, "utf8").trim()
    : "");
const concurrency = Number(env.CHECK_RUNNER_CONCURRENCY ?? "1");
if (!env.CMS_API_URL || !env.CMS_PROJECT || !token || !(concurrency >= 1 && concurrency <= 4))
  throw new Error(
    "CMS_API_URL, CMS_PROJECT, CMS_CHECK_RUNNER_TOKEN(_FILE) and CHECK_RUNNER_CONCURRENCY 1…4 are required",
  );
const config = {
  apiUrl: env.CMS_API_URL,
  project: env.CMS_PROJECT,
  token,
  log: (message: string) => console.log(new Date().toISOString(), message),
};
const client = createClient(config);
const browser = await chromium.launch();
let stopping = false;
for (const signal of ["SIGINT", "SIGTERM"] as const)
  process.on(signal, () => {
    stopping = true;
  });
async function worker() {
  while (!stopping) {
    try {
      if (!(await step(browser, client, config))) await new Promise((r) => setTimeout(r, 2000));
    } catch (error) {
      // A runner failure is never reported as a page result; the lease expires instead.
      config.log(`runner error: ${error instanceof Error ? error.message : String(error)}`);
      await new Promise((r) => setTimeout(r, 2000));
    }
  }
}
config.log(`check runner started for ${config.project}`);
await Promise.all(Array.from({ length: concurrency }, worker));
await browser.close();
