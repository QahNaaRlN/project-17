import { expect, it } from "vitest";
import { atom, designDeclarations, responsive } from "../src/design.js";
it("compiles spacing, color, radius, container tokens and explicit raw values", () => {
  const tokens = {
    spacing: { small: "4px" },
    colors: { brand: "#aabbcc" },
    radius: { round: "8px" },
    container: { wide: "1200px" },
  };
  expect(designDeclarations("padding", "small", { tokens })).toBe(
    "--cms-spacing-small:4px;padding:var(--cms-spacing-small)",
  );
  expect(designDeclarations("background", "brand", { tokens })).toContain(
    "--cms-colors-brand:#aabbcc",
  );
  expect(designDeclarations("radius", "round", { tokens })).toContain("border-radius:var(");
  expect(designDeclarations("maxWidth", "wide", { tokens })).toContain("max-width:var(");
  for (const property of [
    "paddingX",
    "paddingY",
    "paddingTop",
    "paddingBottom",
    "paddingLeft",
    "paddingRight",
    "gap",
    "rowGap",
    "columnGap",
    "marginTop",
    "marginBottom",
  ])
    expect(designDeclarations(property, { raw: 12 }, {})).toContain("12px");
  expect(designDeclarations("width", { raw: "50%" }, {})).toBe("width:50%");
  expect(designDeclarations("color", { raw: "currentColor" }, {})).toBe("color:currentColor");
  for (const [value, expected] of [
    ["auto", "auto"],
    ["full", "100%"],
    ["fit", "fit-content"],
  ])
    expect(designDeclarations("width", value, {})).toBe(`width:${expected}`);
  for (const value of ["left", "center", "right", "justify"])
    expect(designDeclarations("textAlign", value, {})).toBe(`text-align:${value}`);
  expect(designDeclarations("hidden", true, {})).toBe("display:none");
  expect(designDeclarations("hidden", false, {})).toBe("display:revert");
  expect(designDeclarations("wrap", true, {})).toBe("flex-wrap:wrap");
  expect(designDeclarations("wrap", false, {})).toBe("flex-wrap:nowrap");
  expect(designDeclarations("direction", "horizontal", {})).toBe("flex-direction:row");
});
it.each([
  ["padding", 1],
  ["padding", { raw: -1 }],
  ["padding", { raw: 4001 }],
  ["padding", { raw: Infinity }],
  ["padding", { raw: "4001px" }],
  ["width", { raw: "101%" }],
  ["padding", { raw: "-2px" }],
  ["padding", { raw: "calc(100% - 1px)" }],
  ["padding", { raw: "4px", extra: true }],
  ["color", { raw: "url(https://evil.test)" }],
  ["color", { raw: "#abcd" }],
  ["color", { raw: 1 }],
  ["color", "missing"],
  ["hidden", "false"],
  ["textAlign", "unsafe"],
  ["position", "fixed"],
  ["states", {}],
  ["wrap", 1],
  ["direction", "diagonal"],
])("rejects unsupported/unsafe design %s %j", (property, value) =>
  expect(() => designDeclarations(String(property), value, {})).toThrow(),
);
it("validates token values and own properties", () => {
  expect(() =>
    designDeclarations("background", "brand", {
      tokens: { colors: { brand: "red;position:fixed" } },
    }),
  ).toThrow();
  expect(() => designDeclarations("gap", "constructor", { tokens: { spacing: {} } })).toThrow();
  for (const value of ["0", "1rem", "2vw"])
    expect(designDeclarations("padding", { raw: value }, {})).toBe(`padding:${value}`);
  expect(designDeclarations("background", { raw: "transparent" }, {})).toBe(
    "background:transparent",
  );
});
it("enforces responsive base and declared bounded breakpoints with ascending order", () => {
  expect(
    responsive({ lg: "x", base: "base", sm: "y" }, { breakpoints: { sm: 640, lg: 1024 } }),
  ).toEqual([
    ["base", undefined],
    ["y", 640],
    ["x", 1024],
  ]);
  for (const value of [null, 1, "raw", [1], { raw: 12 }])
    expect(responsive(value, {})).toEqual([[value, undefined]]);
  expect(() => responsive({ sm: 1 }, { breakpoints: { sm: 640 } })).toThrow();
  for (const point of [undefined, 0, 4001, 1.5])
    expect(() =>
      responsive({ base: 1, sm: 2 }, { breakpoints: { sm: point as number } }),
    ).toThrow();
  expect(() =>
    responsive({ base: 1, "bad-name": 2 }, { breakpoints: { "bad-name": 640 } }),
  ).toThrow();
  expect(() => responsive({ base: 1, constructor: 2 }, { breakpoints: {} })).toThrow();
});
it("hashes deterministic atoms including breakpoints", () => {
  const first = atom("padding:2px");
  expect(first).toEqual(atom("padding:2px"));
  expect(first.className).toMatch(/^c-[0-9a-f]+$/);
  expect(atom("padding:2px", 640).className).not.toBe(first.className);
  expect(atom("padding:3px").className).not.toBe(first.className);
  expect(atom("padding:2px", 640).css).toContain("@media(min-width:640px)");
});
