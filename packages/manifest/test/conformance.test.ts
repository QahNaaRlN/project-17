import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { DiagnosticCode, manifestHash, validateManifest } from "../src/index.js";

const dir = fileURLToPath(new URL("../fixtures/", import.meta.url));
const load = (path: string): unknown => JSON.parse(readFileSync(dir + path, "utf8"));
const files = (sub: string) =>
  readdirSync(dir + sub).filter((f) => f.endsWith(".json") && f !== "hashes.json");

describe("conformance: valid", () => {
  const hashes = load("valid/hashes.json") as Record<string, string>;
  for (const file of files("valid")) {
    it(file, async () => {
      const manifest = load(`valid/${file}`);
      expect(validateManifest(manifest)).toEqual({ valid: true, diagnostics: [] });
      expect(await manifestHash(manifest)).toBe(hashes[file]);
    });
  }
  it("хэши есть для всех файлов", () => {
    expect(Object.keys(hashes).sort()).toEqual(files("valid").sort());
  });
});

interface InvalidFixture {
  description: string;
  expect: { code: string; pointer: string }[];
  manifest: unknown;
}

describe("conformance: invalid", () => {
  const seen = new Set<string>();
  for (const file of files("invalid")) {
    const fixture = load(`invalid/${file}`) as InvalidFixture;
    it(`${file}: ${fixture.description}`, () => {
      const result = validateManifest(fixture.manifest);
      expect(result.valid).toBe(false);
      for (const want of fixture.expect) {
        seen.add(want.code);
        expect(result.diagnostics).toContainEqual(expect.objectContaining(want));
      }
    });
  }
  it("каждый код диагностики покрыт фикстурой", () => {
    expect([...seen].sort()).toEqual(Object.values(DiagnosticCode).sort());
  });
});
