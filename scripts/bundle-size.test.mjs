// NFR-017: the client bundle of @cms/core + @cms/runtime + @cms/react is under 35 KB gzip.
// Runs after `pnpm build`; counts every emitted module of the three packages, server entry
// points included, so the figure is an upper bound of what an application ships.
import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { readFileSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { test } from "node:test";
import { gzipSync } from "node:zlib";

const root = resolve(import.meta.dirname, "..");
const LIMIT = 35 * 1024;

test("NFR-017: @cms/core, @cms/runtime and @cms/react fit 35 KB gzip", () => {
  let total = 0;
  for (const pkg of ["sdk-core", "runtime", "react"]) {
    const dist = join(root, "packages", pkg, "dist");
    const files = readdirSync(dist, { recursive: true }).filter((f) => String(f).endsWith(".js"));
    assert.ok(files.length > 0, `${pkg}: run pnpm build first`);
    total += gzipSync(Buffer.concat(files.map((f) => readFileSync(join(dist, String(f)))))).length;
  }
  assert.ok(total < LIMIT, `${total} bytes gzip, limit ${LIMIT}`);
});
