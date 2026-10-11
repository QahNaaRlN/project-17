import { describe, expect, it } from "vitest";
import type { Predicate } from "@cms/ir";
import {
  evaluateBinding,
  evaluatePredicate,
  formatValue,
  readPath,
  type BindingScope,
} from "../src/bindings.js";

const scope: BindingScope = {
  content: { name: "Дом", zero: 0, no: false, blank: "", nil: null, rows: [{ id: "one" }] },
  index: 0,
  item: { id: "one" },
  each: { outer: { id: "out" } },
  data: { bad: { error: { code: "FAILED" } } },
  context: { locale: "ru" },
};
describe("IR-040: safe paths and bindings", () => {
  it.each([
    ["$content.name", "Дом"],
    ["$content.rows.0.id", "one"],
    ["$index", 0],
    ["$each.outer.id", "out"],
    ["$data.bad.error.code", "FAILED"],
  ])("reads %s", (path, value) => expect(readPath(String(path), scope)).toBe(value));
  it.each([
    "content.name",
    "$content[0]",
    "$content.name()",
    "$content.constructor",
    "$content.rows.0.__proto__",
    "$content.toString",
    "$content.missing.deep",
    "$content.nil.deep",
    "$context.locale.length",
    "$content.rows.01",
    "$unknown",
  ])("rejects %s", (path) => expect(readPath(path, scope)).toBeUndefined());
  it("does not traverse inherited roots", () =>
    expect(readPath("$content.name", Object.create(scope))).toBeUndefined());
  it.each(["zero", "no", "blank"])("preserves %s instead of default", (key) =>
    expect(evaluateBinding({ expr: `$content.${key}`, default: "fallback" }, scope)).toBe(
      readPath(`$content.${key}`, scope),
    ),
  );
  it("defaults only absent/null and applies format", () => {
    expect(
      evaluateBinding(
        { expr: "$content.nil", default: "word", format: { fn: "uppercase" } },
        scope,
      ),
    ).toBe("WORD");
    expect(evaluateBinding({ expr: "$content.missing", default: 0 }, scope)).toBe(0);
    expect(evaluateBinding("$content.name", scope)).toBe("Дом");
    expect(evaluateBinding({ expr: "$content.missing" }, scope)).toBeUndefined();
  });
  it("substitutes scalar template vars without evaluating code", () => {
    expect(
      evaluateBinding(
        {
          template: "{name}: {zero} / {no} / {nil}",
          vars: {
            name: "$content.name",
            zero: "$content.zero",
            no: "$content.no",
            nil: "$content.nil",
          },
        },
        scope,
      ),
    ).toBe("Дом: 0 / false / ");
    expect(
      evaluateBinding(
        { template: "{x}", vars: { x: { expr: "$content.none", default: "ok" } } },
        scope,
      ),
    ).toBe("ok");
  });
  it.each([
    { template: "{x}", vars: {} },
    { template: "{x}", vars: { x: "$content.rows" } },
  ])("rejects invalid template", (binding) =>
    expect(() => evaluateBinding(binding, scope)).toThrow("CMS_TEMPLATE_INVALID"),
  );
});
describe("formatters", () => {
  it("formats number/currency and respects explicit precision", () => {
    expect(
      formatValue(
        0.125,
        { fn: "number", args: { style: "percent", minFraction: 1, maxFraction: 1 } },
        "en",
      ),
    ).toBe("12.5%");
    expect(formatValue(1234.5, { fn: "number" }, "en")).toBe("1,234.5");
    expect(formatValue(12, { fn: "currency", args: { code: "USD" } }, "en")).toBe("$12.00");
    expect(formatValue(12, { fn: "currency", args: { code: "JPY" } }, "en")).toBe("¥12");
  });
  it.each(["short", "medium", "long", "iso"])("formats UTC date %s", (style) => {
    const result = formatValue("2020-12-31T23:30:00-01:00", { fn: "date", args: { style } }, "en");
    expect(result).toBe(
      style === "iso"
        ? "2021-01-01T00:30:00.000Z"
        : new Intl.DateTimeFormat("en", { dateStyle: style as "short", timeZone: "UTC" }).format(
            new Date("2021-01-01T00:30:00Z"),
          ),
    );
  });
  it("uses default locale/style, null values and Unicode", () => {
    expect(formatValue("2020-01-01", { fn: "date" })).toBe("Jan 1, 2020");
    expect(formatValue(null, { fn: "unknown" })).toBeNull();
    expect(formatValue(undefined, { fn: "unknown" })).toBeUndefined();
    expect(formatValue("AЯ", { fn: "lowercase" }, "ru")).toBe("aя");
    expect(formatValue("🙂ab", { fn: "truncate", args: { length: 2 } })).toBe("🙂a");
    expect(formatValue("abc", { fn: "truncate" })).toBe("abc");
    expect(formatValue("abc", { fn: "truncate", args: { length: 0 } })).toBe("");
  });
  it.each([
    [1, { fn: "uppercase" }],
    [1, { fn: "lowercase" }],
    [1, { fn: "truncate" }],
    ["a", { fn: "truncate", args: { length: -1 } }],
    ["a", { fn: "truncate", args: { length: 1.5 } }],
    ["a", { fn: "truncate", args: { length: 100001 } }],
    ["1", { fn: "number" }],
    [Infinity, { fn: "number" }],
    [1, { fn: "number", args: { style: "scientific" } }],
    [1, { fn: "number", args: { minFraction: 21 } }],
    [1, { fn: "number", args: { maxFraction: -1 } }],
    [1, { fn: "currency" }],
    [1, { fn: "currency", args: { code: "usd" } }],
    [1, { fn: "currency", args: { code: 1 } }],
    [1, { fn: "date" }],
    ["31/01/2020", { fn: "date" }],
    ["2020-15-01", { fn: "date" }],
    ["2021-02-29", { fn: "date" }],
    [1, { fn: "number", args: { style: "currency", code: "USD" } }],
    ["2020-01-01", { fn: "date", args: { style: "full" } }],
    ["a", { fn: "unknown" }],
  ])("rejects invalid formatter input %j", (value, format) =>
    expect(() => formatValue(value, format as { fn: string })).toThrow(),
  );
});
describe("predicates", () => {
  const eq: Predicate = { op: "eq", args: ["$content.zero", { lit: 0 }] };
  it.each([
    ["exists", "$content.zero", true],
    ["exists", "$content.no", true],
    ["exists", "$content.nil", false],
    ["empty", "$content.blank", true],
    ["empty", "$content.nil", true],
    ["empty", "$content.rows", false],
    ["empty", "$content.zero", false],
  ])("%s %s", (op, arg, result) =>
    expect(evaluatePredicate({ op, arg } as Predicate, scope)).toBe(result),
  );
  it("treats loader failures as absent collection and handles empty arrays", () => {
    expect(evaluatePredicate({ op: "exists", arg: "$data.bad.rows" }, scope)).toBe(false);
    expect(evaluatePredicate({ op: "empty", arg: "$data.bad.rows" }, scope)).toBe(true);
    expect(
      evaluatePredicate({ op: "empty", arg: "$content.rows" }, { content: { rows: [] } }),
    ).toBe(true);
  });
  it.each(["eq", "neq", "gt", "gte", "lt", "lte"])("compares with %s", (op) => {
    const results: Record<string, boolean[]> = {
      eq: [false, true, false],
      neq: [true, false, true],
      gt: [false, false, true],
      gte: [false, true, true],
      lt: [true, false, false],
      lte: [true, true, false],
    };
    for (const [i, value] of [1, 2, 3].entries())
      expect(
        evaluatePredicate({ op, args: [{ lit: value }, { lit: 2 }] } as Predicate, scope),
      ).toBe(results[op]![i]);
  });
  it("refuses coercion and handles literal equality/in/logic", () => {
    expect(evaluatePredicate({ op: "gt", args: [{ lit: "3" }, { lit: 2 }] }, scope)).toBe(false);
    expect(evaluatePredicate({ op: "gt", args: [{ lit: 3 }, { lit: "2" }] }, scope)).toBe(false);
    expect(evaluatePredicate({ op: "gt", args: [{ lit: Infinity }, { lit: 2 }] }, scope)).toBe(
      false,
    );
    expect(evaluatePredicate({ op: "gt", args: [{ lit: 3 }, { lit: NaN }] }, scope)).toBe(false);
    expect(evaluatePredicate(eq, scope)).toBe(true);
    expect(evaluatePredicate({ op: "not", arg: eq }, scope)).toBe(false);
    expect(
      evaluatePredicate({ op: "and", args: [eq, { op: "exists", arg: "$item" }] }, scope),
    ).toBe(true);
    expect(
      evaluatePredicate({ op: "and", args: [eq, { op: "exists", arg: "$content.nil" }] }, scope),
    ).toBe(false);
    expect(
      evaluatePredicate({ op: "or", args: [{ op: "exists", arg: "$content.nil" }, eq] }, scope),
    ).toBe(true);
    expect(
      evaluatePredicate({ op: "or", args: [{ op: "exists", arg: "$content.nil" }] }, scope),
    ).toBe(false);
    expect(
      evaluatePredicate({ op: "in", args: ["$item.id", { lit: ["one", "two"] }] }, scope),
    ).toBe(true);
    expect(evaluatePredicate({ op: "in", args: ["$item.none", { lit: [undefined] }] }, scope)).toBe(
      false,
    );
    expect(evaluatePredicate({ op: "in", args: ["$item.id", { lit: 1 }] }, scope)).toBe(false);
    for (const value of [null, 1, "not-a-path", { lit: 1, extra: true }]) {
      expect(() => evaluatePredicate({ op: "eq", args: [value, { lit: 1 }] }, scope)).toThrow(
        "CMS_OPERAND_INVALID",
      );
    }
  });
  it("bounds predicate depth/arity and rejects unknown operations", () => {
    let predicate: Predicate = eq;
    for (let i = 0; i < 10; i++) predicate = { op: "not", arg: predicate };
    expect(() => evaluatePredicate(predicate, scope)).toThrow("CMS_PREDICATE_LIMIT");
    for (const args of [[], Array(17).fill(eq)])
      expect(() => evaluatePredicate({ op: "and", args } as Predicate, scope)).toThrow();
    expect(() => evaluatePredicate({ arg: "$item" } as Predicate, scope)).toThrow(
      "CMS_PREDICATE_UNKNOWN",
    );
  });
});
