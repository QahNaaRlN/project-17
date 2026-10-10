import { describe, expect, it } from "vitest";
import { canonicalJson, manifestHash, validateManifest } from "../src/index.js";

describe("MF-002: Unicode", () => {
  for (const value of ["\ud800", "\udfff", "x\ud800y", "\ud800\ud800", "\udfff\udfff"]) {
    it(`rejects ${JSON.stringify(value)} in values and keys`, async () => {
      for (const document of [value, [value], { nested: { value } }, { [value]: "ok" }]) {
        expect(validateManifest(document)).toEqual({
          valid: false,
          diagnostics: [
            {
              code: "MANIFEST_SCHEMA_VIOLATION",
              severity: "error",
              pointer: "",
              message: "manifest содержит некорректный Unicode",
              params: { keyword: "unicode" },
            },
          ],
        });
        expect(() => canonicalJson(document)).toThrow("Unicode");
        await expect(manifestHash(document)).rejects.toThrow("Unicode");
      }
    });
  }
  it("preserves pairs, replacement character and literal escape text", () => {
    for (const value of ["😀", "\ud800\udc00", "�", "\\ud800", "", "abc"]) {
      expect(canonicalJson({ [value]: [value] })).toBe(JSON.stringify({ [value]: [value] }));
    }
  });
});
