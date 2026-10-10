/* eslint-disable @typescript-eslint/no-explicit-any -- тесты меняют произвольные вложенные поля фикстуры */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { DiagnosticCode, validateManifest } from "../src/index.js";
import { pointer } from "../src/diagnostics.js";

const store = JSON.parse(
  readFileSync(fileURLToPath(new URL("../fixtures/valid/store.json", import.meta.url)), "utf8"),
) as Record<string, any>;
const clone = () => structuredClone(store);

describe("validateManifest", () => {
  it("не объект — одна ошибка схемы в корне", () => {
    const r = validateManifest(42);
    expect(r.valid).toBe(false);
    expect(r.diagnostics).toHaveLength(1);
    expect(r.diagnostics[0]).toMatchObject({ code: DiagnosticCode.SchemaViolation, pointer: "" });
  });

  it("ошибки схемы — по одной на место, сообщения по-русски", () => {
    const m = clone();
    m.components.ProductCard.props.badge = { type: "text", maxLength: 0 };
    const r = validateManifest(m);
    expect(r.diagnostics).toEqual([
      expect.objectContaining({
        pointer: "/components/ProductCard/props/badge/maxLength",
        message: expect.stringMatching(/1/),
      }),
    ]);
  });

  it("недопустимое имя свойства указывает на само свойство", () => {
    const m = clone();
    m.capabilities["Bad"] = {};
    expect(validateManifest(m).diagnostics[0]?.pointer).toBe("/capabilities/Bad");
  });

  it("диагностики отсортированы по указателю, затем по коду", () => {
    const m = clone();
    m.components.ProductCard.capabilities = ["x.y"];
    m.components.ProductCard.props.variant.default = "wide";
    m.actions["commerce.addToCart"].args.quantity.default = 0;
    const ptrs = validateManifest(m).diagnostics.map((d) => d.pointer);
    expect(ptrs).toEqual([...ptrs].sort());
    expect(ptrs).toHaveLength(3);
  });

  it("одно место — несколько кодов в порядке кода", () => {
    const m = clone();
    m.components.Image = { capabilities: [] };
    m.primitives.Image = {};
    const codes = validateManifest(m)
      .diagnostics.filter((d) => d.pointer === "/components/Image")
      .map((d) => d.code);
    expect(codes).toEqual([DiagnosticCode.NameDuplicate, DiagnosticCode.NameReserved]);
  });

  it("параметры диагностики", () => {
    const m = clone();
    m.dataSources["commerce.products.list"].params.limit.min = 50;
    m.schemas.Product.migrateFrom["5"] = [{ op: "drop", field: "x" }];
    const d = validateManifest(m).diagnostics;
    expect(d).toContainEqual(
      expect.objectContaining({ code: DiagnosticCode.RangeInvalid, params: { min: 50, max: 48 } }),
    );
    expect(d).toContainEqual(
      expect.objectContaining({
        code: DiagnosticCode.MigrationInvalid,
        params: { from: 5, version: 3 },
      }),
    );
  });

  it("граничные значения допустимы", () => {
    const m = clone();
    const qty = m.actions["commerce.addToCart"].args.quantity;
    qty.default = 99;
    m.primitives.PriceTag.props.currency.default = "USD";
    m.components.ProductCard.props.badge.default = "я".repeat(40);
    m.components.ProductCard.slots.footer.min = 2;
    m.schemas.Product.migrateFrom["2"] = [{ op: "drop", field: "x" }];
    m.dataSources["commerce.products.list"].params.limit.min = 48;
    m.dataSources["commerce.products.list"].params.limit.default = 48;
    expect(validateManifest(m).diagnostics).toEqual([]);
    qty.default = 1;
    qty.min = 1;
    expect(validateManifest(m).valid).toBe(true);
  });

  it("default без ограничений допустим; unique у text и number", () => {
    const m = clone();
    m.components.Gallery.props.caption = { type: "string", default: "x" };
    m.components.Gallery.props.size = { type: "number", default: -5 };
    m.components.Gallery.props.title = { type: "text", default: "длинный текст" };
    m.schemas.Product.fields.sku = { type: "text", unique: true };
    m.schemas.Product.fields.code = { type: "number", unique: true };
    expect(validateManifest(m).diagnostics).toEqual([]);
  });

  it("content допустим у text, richText, asset, link", () => {
    const m = clone();
    m.components.Gallery.props = {
      a: { type: "text", content: true },
      b: { type: "richText", content: true },
      c: { type: "asset", content: true },
      d: { type: "link", content: true },
    };
    expect(validateManifest(m).diagnostics).toEqual([]);
  });

  it("одна поддерживаемая версия IR среди нескольких достаточна", () => {
    const m = clone();
    m.irVersions = ["0.9", "1.0"];
    expect(validateManifest(m).valid).toBe(true);
  });

  it("проверяет вложенные поля объекта, события и provides", () => {
    const m = clone();
    m.components.ProductCard.events.select = {
      payload: { item: { type: "reference", schema: "Nope" } },
    };
    m.components.ProductCard.provides.extra = { type: "enum", values: ["a"], default: "b" };
    const codes = validateManifest(m).diagnostics.map((d) => [d.code, d.pointer]);
    expect(codes).toEqual([
      [DiagnosticCode.UnknownSchema, "/components/ProductCard/events/select/payload/item/schema"],
      [DiagnosticCode.DefaultInvalid, "/components/ProductCard/provides/extra/default"],
    ]);
  });
});

describe("pointer", () => {
  it("экранирует ~ и / (RFC 6901)", () => {
    expect(pointer("a~b/c", 0)).toBe("/a~0b~1c/0");
  });
});
