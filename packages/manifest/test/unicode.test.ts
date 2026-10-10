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

describe("MF-002: U+0000 не сохраняется в JSONB", () => {
  it("отклоняет NUL в значениях и ключах до канонизации", async () => {
    for (const value of ["\0", "a\0b"]) {
      for (const document of [value, [value], { nested: { value } }, { [value]: "ok" }]) {
        expect(validateManifest(document)).toMatchObject({
          valid: false,
          diagnostics: [
            { code: "MANIFEST_SCHEMA_VIOLATION", pointer: "", params: { keyword: "nul" } },
          ],
        });
        expect(() => canonicalJson(document)).toThrow("U+0000");
        await expect(manifestHash(document)).rejects.toThrow("U+0000");
      }
    }
  });
  it("сохраняет буквальный escape и остальные управляющие символы", () => {
    for (const value of ["\\u0000", "\n\t\b\f\r", "😀"]) {
      expect(canonicalJson({ [value]: value })).toBe(JSON.stringify({ [value]: value }));
    }
  });
});
