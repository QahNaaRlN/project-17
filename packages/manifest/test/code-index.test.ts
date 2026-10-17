import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import { codeIndexPath, validateCodeIndex, canonicalJson, manifestHash } from "../src/index.js";
const fixture = () =>
  JSON.parse(readFileSync(new URL("../fixtures/code-index.json", import.meta.url), "utf8"));
it("STU-263/264: shared metadata fixture is bounded, deterministic and contains no source code", async () => {
  const value = fixture();
  expect(validateCodeIndex(value)).toBe(true);
  expect(validateCodeIndex(JSON.parse(canonicalJson(value)))).toBe(true);
  expect(await manifestHash(value)).toBe(await manifestHash(JSON.parse(canonicalJson(value))));
  for (const path of [
    "",
    "/root",
    "../a.ts",
    "a/../b.ts",
    "a/./b",
    "a//b",
    "a/",
    "C:/a.ts",
    "a\\b.ts",
    "node_modules/a.ts",
    "Dist/a",
    ".git/config",
    ".env",
    "a/.env.local",
    "a\n.ts",
    "x".repeat(513),
  ])
    expect(codeIndexPath(path), path).toBe(false);
  for (const path of ["src/a.ts", "src/русский.ts", "packages/@cms/index.ts"])
    expect(codeIndexPath(path)).toBe(true);
  for (const invalid of [
    null,
    [],
    {},
    { ...value, extra: "source" },
    { ...value, repo: { root: ".", commit: "main" } },
    { ...value, conventions: { ...value.conventions, registry: "../secret" } },
    { ...value, elements: null },
  ])
    expect(validateCodeIndex(invalid)).toBe(false);
  for (const mutation of [
    { definition: { file: "src/a.ts", line: 0 } },
    { implementation: { file: "src/a.ts", export: "f", line: 0 } },
    { implementation: { file: "src/a.ts", export: "f", line: null } },
    { dependencies: ["a", "a"] },
    { tests: ["/secret"] },
    { source: "private" },
  ]) {
    const v = fixture();
    Object.assign(v.elements["action:store.favorite"], mutation);
    expect(validateCodeIndex(v)).toBe(false);
  }
  const empty = { ...value, elements: {} };
  expect(validateCodeIndex(empty)).toBe(true);
});
