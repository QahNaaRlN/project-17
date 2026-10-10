import fc from "fast-check";
import { hasNUL } from "../src/unicode.js";
import { describe, expect, it } from "vitest";
import { canonicalJson, manifestHash } from "../src/index.js";

describe("canonicalJson", () => {
  it("ключи по возрастанию, без пробелов, undefined пропускается", () => {
    expect(canonicalJson({ b: 1, a: [true, null, "x"], c: undefined, "": { z: 0, y: -1.5 } })).toBe(
      '{"":{"y":-1.5,"z":0},"a":[true,null,"x"],"b":1}',
    );
  });

  it("строки и числа — как у JSON.stringify", () => {
    expect(canonicalJson(' <&>"\\\n\u0001😀')).toBe(JSON.stringify(' <&>"\\\n\u0001😀'));
    expect(canonicalJson(1e21)).toBe("1e+21");
    expect(canonicalJson(0.1)).toBe("0.1");
  });

  it("уже упорядоченные и обратные ключи дают один порядок", () => {
    const keys = ["a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"];
    const sorted = `{${keys.map((k) => `"${k}":0`).join(",")}}`;
    expect(canonicalJson(Object.fromEntries(keys.map((k) => [k, 0])))).toBe(sorted);
    expect(canonicalJson(Object.fromEntries([...keys].reverse().map((k) => [k, 0])))).toBe(sorted);
  });

  it("порядок ключей по кодовым единицам UTF-16", () => {
    expect(canonicalJson({ "￿": 1, "😀": 2, b: 3 })).toBe('{"b":3,"😀":2,"￿":1}');
  });

  it("свойство: результат — корректный JSON того же значения и не зависит от порядка ключей", () => {
    fc.assert(
      fc.property(fc.jsonValue(), (value) => {
        if (hasNUL(value)) {
          // MF-002: NUL должен отклоняться, а не исчезать при канонизации.
          expect(() => canonicalJson(value)).toThrow("U+0000");
          return;
        }
        const text = canonicalJson(value);
        expect(JSON.parse(text)).toEqual(JSON.parse(JSON.stringify(value)));
        const shuffled = JSON.parse(JSON.stringify(value), (_k, v: unknown) =>
          v !== null && typeof v === "object" && !Array.isArray(v)
            ? Object.fromEntries(Object.entries(v).reverse())
            : v,
        );
        expect(canonicalJson(shuffled)).toBe(text);
      }),
    );
  });
});

describe("manifestHash", () => {
  it("sha256 канонического JSON", async () => {
    expect(await manifestHash({})).toBe(
      "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
    );
    expect(await manifestHash({ a: 1, b: 2 })).toBe(await manifestHash({ b: 2, a: 1 }));
  });
});
