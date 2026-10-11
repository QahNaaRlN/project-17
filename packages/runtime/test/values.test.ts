import { expect, it } from "vitest";
import fc from "fast-check";
import type { Type } from "@cms/manifest";
import { fieldValue, fieldValues, jsonValue } from "../src/values.js";
import { sourceResult, loadSource } from "../src/sources.js";
const uuid = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
const schemas: [Type, unknown, unknown[]][] = [
  [
    { type: "string", minLength: 2, maxLength: 3, pattern: "^[a-z]+$" },
    "abc",
    [1, "a", "abcd", "ABC", "a\0b"],
  ],
  [{ type: "string", pattern: "(?=a)" }, undefined, ["a"]],
  [{ type: "text", maxLength: 2 }, "аб", ["abc", true]],
  [{ type: "number", integer: true, min: 1, max: 3 }, 2, [0, 4, 1.5, "2", Infinity]],
  [{ type: "number" }, 1.5, [NaN]],
  [{ type: "boolean" }, false, [0]],
  [{ type: "enum", values: ["one", "two"] }, "one", ["three"]],
  [{ type: "date" }, "2024-02-29", ["2023-02-29", "2024-13-01", "2024-1-1", "bad"]],
  [
    { type: "datetime" },
    "2024-02-29T12:00:00Z",
    ["2024-02-29", "2024-02-30T12:00:00Z", "2024-01-01T99:99:99Z"],
  ],
  [
    { type: "url", schemes: ["https"] },
    "https://example.test/x",
    ["http://example.test", "javascript:bad", "//evil.test"],
  ],
  [{ type: "url" }, "/relative", ["https://user:pass@example.test"]],
  [
    { type: "link" },
    { kind: "url", url: "/safe" },
    [
      null,
      "url",
      { kind: "url", url: "javascript:bad" },
      { kind: "url", url: "/safe", other: true },
    ],
  ],
  [{ type: "link" }, { kind: "anchor", node: "target" }, [{ kind: "anchor", node: "x" }]],
  [
    { type: "link" },
    { kind: "page", page: uuid, params: { slug: "one" } },
    [
      { kind: "page", page: "a".repeat(36) },
      { kind: "page", page: uuid, params: [] },
      { kind: "page", page: uuid, secret: true },
    ],
  ],
  [
    { type: "asset" },
    { assetId: uuid, alt: "Image" },
    [{ assetId: 1 }, { assetId: uuid, alt: 1 }, { assetId: uuid, secret: true }],
  ],
  [
    { type: "reference", schema: "Product" },
    { entityId: uuid },
    [{ entityId: "bad" }, { entityId: uuid, alt: "bad" }],
  ],
  [
    { type: "list", of: { type: "boolean" }, min: 1, max: 2 },
    [true],
    [[], [true, false, true], [1], {}],
  ],
  [{ type: "list", of: { type: "string" } }, [], ["list"]],
  [
    { type: "object", fields: { title: { type: "string", required: true } } },
    { title: "one" },
    [[], {}, { title: "one", extra: 1 }],
  ],
  [{ type: "color" }, "surface", [1]],
  [{ type: "nodeRef" }, "target", ["a", "bad space"]],
];
it.each(schemas)("IR-052/SDK-014: проверяет %j и её ограничения", (schema, valid, invalid) => {
  if (valid !== undefined) expect(fieldValue(schema, valid)).toEqual(valid);
  for (const value of invalid) {
    if (value === null && !schema.required) continue;
    expect(() => fieldValue(schema, value)).toThrow("CMS_VALUE_INVALID");
  }
});
it("IR-052: отсутствие сохраняется, defaults и обязательность проверяются на каждом уровне", () => {
  expect(
    fieldValues({ name: { type: "string", default: "one" }, optional: { type: "boolean" } }, {}),
  ).toEqual({ name: "one" });
  expect(fieldValue({ type: "string" }, null)).toBeNull();
  expect(() => fieldValue({ type: "string", required: true }, null)).toThrow();
  expect(() => fieldValues({ name: { type: "string" } }, { extra: true })).toThrow();
  expect(fieldValue({ type: "text" }, "no bound")).toBe("no bound");
  expect(fieldValue({ type: "link" }, { kind: "page", page: uuid })).toEqual({
    kind: "page",
    page: uuid,
  });
});
it("SDK-014: снимки JSON отклоняют исполняемые значения, getters, циклы и превышение лимитов", () => {
  const cyclic: Record<string, unknown> = {};
  cyclic.self = cyclic;
  let getterCalls = 0;
  const accessor = Object.defineProperty({}, "secret", {
    enumerable: true,
    get() {
      getterCalls++;
      return "secret";
    },
  });
  const sparse = Array(2);
  for (const value of [
    undefined,
    () => null,
    Infinity,
    Symbol("x"),
    new Date(),
    cyclic,
    accessor,
    { [Symbol("x")]: 1 },
    sparse,
    JSON.parse('{"__proto__":1}'),
    { constructor: true },
    { prototype: true },
    "a".repeat(2 * 1024 * 1024 + 1),
    Array(100_001).fill(0),
  ])
    expect(() => jsonValue(value)).toThrow("CMS_VALUE_INVALID");
  let deep: unknown = 1;
  for (let i = 0; i < 33; i++) deep = { child: deep };
  expect(() => jsonValue(deep)).toThrow();
  expect(getterCalls).toBe(0);
  expect(jsonValue({ one: true, two: null, three: [1, "x"] })).toEqual({
    one: true,
    two: null,
    three: [1, "x"],
  });
  function hasReservedKey(input: unknown): boolean {
    return (
      input !== null &&
      typeof input === "object" &&
      (Object.keys(input).some((key) => ["__proto__", "prototype", "constructor"].includes(key)) ||
        Object.values(input).some(hasReservedKey))
    );
  }
  fc.assert(
    fc.property(fc.jsonValue(), (value) => {
      if (hasReservedKey(value)) expect(() => jsonValue(value)).toThrow("CMS_VALUE_INVALID");
      else expect(jsonValue(value)).toEqual(value);
    }),
    { numRuns: 50, seed: -2060770988 },
  );
});
it("SDK-014: произвольный результат загрузчика даёт значение или безопасный код без исключения", async () => {
  await fc.assert(
    fc.asyncProperty(fc.anything(), async (value) => {
      const result = await loadSource(
        { source: "test.value" },
        [
          {
            name: "test.value",
            params: {},
            result: { type: "string", required: true },
            loader: () => value,
          },
        ],
        {},
      );
      if (typeof value === "string" && !value.includes("\0")) expect(result).toBe(value);
      else expect(result).toEqual({ error: { code: "CMS_SOURCE_RESULT_INVALID" } });
    }),
    { numRuns: 50 },
  );
});
const rich: Type = { type: "richText" };
it("SDK-014: JSON не исполняет унаследованный итератор массива", () => {
  let calls = 0;
  const value = ["one"];
  Object.setPrototypeOf(value, {
    [Symbol.iterator]() {
      calls++;
      return ["replacement"][Symbol.iterator]();
    },
  });
  expect(() => jsonValue(value)).toThrow("CMS_VALUE_INVALID");
  expect(calls).toBe(0);
});
const paragraph = {
  type: "paragraph",
  content: [
    {
      type: "text",
      text: "Safe",
      marks: [{ type: "bold" }, { type: "link", attrs: { to: { kind: "url", url: "/safe" } } }],
    },
  ],
};
it("CNT-030/SDK-014: RichText v1 принимает только структурные блоки, marks и безопасные атрибуты", () => {
  const value = {
    type: "doc",
    content: [
      paragraph,
      {
        type: "heading",
        attrs: { level: 2 },
        content: [{ type: "text", text: "Title" }, { type: "hardBreak" }],
      },
      { type: "bulletList", content: [{ type: "listItem", content: [paragraph] }] },
      { type: "orderedList", content: [{ type: "listItem", content: [paragraph] }] },
      { type: "blockquote", content: [paragraph] },
      { type: "image", attrs: { assetId: uuid, alt: "Image" } },
      { type: "divider" },
    ],
  };
  expect(fieldValue(rich, value)).toEqual(value);
  expect(
    fieldValue(
      { type: "richText", blocks: ["paragraph"], marks: ["bold", "link"] },
      { type: "doc", content: [paragraph] },
    ),
  ).toEqual({ type: "doc", content: [paragraph] });
  for (const input of [
    "html",
    {},
    { type: "doc", content: "bad" },
    { type: "doc", content: [{ type: "text", text: "bad" }] },
    { type: "doc", content: [{ type: "listItem", content: [] }] },
    { type: "doc", content: [{ type: "doc", content: [] }] },
    { type: "doc", content: [{ type: "html" }] },
    { type: "doc", content: [{ type: "heading", attrs: { level: 1 }, content: [] }] },
    { type: "doc", content: [{ type: "paragraph", attrs: {}, content: [] }] },
    { type: "doc", content: [{ type: "paragraph", content: [null] }] },
    {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "image", attrs: { assetId: uuid } }] }],
    },
    { type: "doc", content: [{ type: "bulletList", content: [paragraph] }] },
    { type: "doc", content: [{ type: "divider", content: [] }] },
    { type: "doc", content: [{ type: "paragraph" }] },
    { type: "doc", content: [{ type: "paragraph", text: "bad", content: [] }] },
    { type: "doc", content: [{ type: "paragraph", marks: [], content: [] }] },
    {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "text", content: [] }] }],
    },
    {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "text", attrs: {} }] }],
    },
  ])
    expect(() => fieldValue(rich, input)).toThrow();
  for (const marks of [
    1,
    [null],
    [{ type: "html" }],
    [{ type: "bold", attrs: {} }],
    [{ type: "link" }],
    [{ type: "link", attrs: { to: { kind: "url", url: "javascript:bad" } } }],
    [{ type: "bold", extra: true }],
  ])
    expect(() =>
      fieldValue(rich, {
        type: "doc",
        content: [{ type: "paragraph", content: [{ type: "text", text: "x", marks }] }],
      }),
    ).toThrow();
  expect(() =>
    fieldValue({ type: "richText", blocks: [] }, { type: "doc", content: [paragraph] }),
  ).toThrow();
  expect(() =>
    fieldValue({ type: "richText", marks: [] }, { type: "doc", content: [paragraph] }),
  ).toThrow();
  expect(() => fieldValue(rich, { type: "doc", extra: true, content: [] })).toThrow();
  expect(() =>
    fieldValue(rich, {
      type: "doc",
      content: [{ type: "heading", attrs: { level: 2, extra: true }, content: [] }],
    }),
  ).toThrow();
  expect(() =>
    fieldValue(rich, {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: 1 }] }],
    }),
  ).toThrow();
  expect(
    sourceResult({ name: "test.rich", params: {}, result: rich, loader: () => null }, value),
  ).toEqual(value);
});
