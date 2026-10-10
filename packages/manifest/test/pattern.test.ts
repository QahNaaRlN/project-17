/* eslint-disable @typescript-eslint/no-explicit-any -- мутации JSON-фикстуры */
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import fc from "fast-check";
import { compilePattern, validateManifest } from "../src/index.js";

const cases = JSON.parse(
  readFileSync(new URL("../fixtures/patterns.json", import.meta.url), "utf8"),
) as {
  pattern: string;
  valid: boolean;
  matches: [string, boolean][];
}[];
const store = JSON.parse(
  readFileSync(new URL("../fixtures/valid/store.json", import.meta.url), "utf8"),
) as any;

describe("MF-001 portable pattern", () => {
  for (const [index, c] of cases.entries()) {
    it(`shared case ${index}`, () => {
      const compiled = compilePattern(c.pattern);
      expect(compiled !== undefined).toBe(c.valid);
      for (const [value, want] of c.matches) expect(compiled!.test(value)).toBe(want);
      const m = structuredClone(store);
      m.components.ProductCard.props.test = { type: "string", pattern: c.pattern };
      const result = validateManifest(m);
      expect(result.valid).toBe(c.valid);
      if (!c.valid && !c.pattern.includes("\0") && [...c.pattern].length <= 500)
        expect(result.diagnostics).toContainEqual(
          expect.objectContaining({
            code: "MANIFEST_MODIFIER_INVALID",
            pointer: "/components/ProductCard/props/test/pattern",
            params: { reason: "unsupported_pattern" },
          }),
        );
    });
  }
  it("rejects invalid default and reports length violation once", () => {
    const m = structuredClone(store);
    m.components.ProductCard.props.test = { type: "string", pattern: "^x$", default: "y" };
    expect(validateManifest(m).diagnostics).toEqual([
      expect.objectContaining({
        code: "MANIFEST_DEFAULT_INVALID",
        pointer: "/components/ProductCard/props/test/default",
      }),
    ]);
    m.components.ProductCard.props.test.minLength = 2;
    expect(validateManifest(m).diagnostics).toHaveLength(1);
    m.components.ProductCard.props.test.default = "x";
    delete m.components.ProductCard.props.test.minLength;
    expect(validateManifest(m).valid).toBe(true);
  });
  it("literal scalar round trip and rejection of every special group", () => {
    fc.assert(
      fc.property(fc.array(fc.constantFrom("a", "я", "😀", "é"), { maxLength: 40 }), (chars) => {
        const value = chars.join("");
        expect(compilePattern("^" + value + "$")!.test(value)).toBe(true);
        expect(compilePattern("(?=" + value + ")")).toBeUndefined();
      }),
    );
  });
});
