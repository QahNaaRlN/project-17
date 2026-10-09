import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { validateDocument } from "../src/index.js";

const fixturesDir = fileURLToPath(new URL("../fixtures/", import.meta.url));

function load(kind: "valid" | "invalid"): [string, unknown][] {
  const dir = `${fixturesDir}${kind}/`;
  return readdirSync(dir)
    .filter((f) => f.endsWith(".json"))
    .sort()
    .map((f) => [f.replace(/\.json$/, ""), JSON.parse(readFileSync(dir + f, "utf8"))]);
}

interface InvalidFixture {
  description: string;
  expect: { code: string; pointer?: string }[];
  document: unknown;
}

describe("conformance: valid", () => {
  it.each(load("valid"))("%s", (_name, doc) => {
    expect(validateDocument(doc).diagnostics).toEqual([]);
  });
});

describe("conformance: invalid", () => {
  it.each(load("invalid") as [string, InvalidFixture][])("%s", (_name, fixture) => {
    const result = validateDocument(fixture.document);
    expect(result.valid).toBe(false);
    for (const expected of fixture.expect) {
      const found = result.diagnostics.some(
        (d) =>
          d.code === expected.code &&
          (expected.pointer === undefined || d.pointer === expected.pointer),
      );
      expect(
        found,
        `${JSON.stringify(expected)} in ${JSON.stringify(result.diagnostics, null, 2)}`,
      ).toBe(true);
    }
  });
});
