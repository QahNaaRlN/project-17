import { describe, it, expect } from "vitest";
import fc from "fast-check";
import { parseCatalog, priceLabel, serializeModel } from "../src/model.js";
import { catalog } from "../scripts/catalog.js";
import { page, id, model } from "./fixtures.js";

describe("STORE-001/006/007: контракт витрины", () => {
  it("читает полный контент, изображения и цену ноль", () => {
    const c = structuredClone(catalog);
    c.heroAsset = { assetId: id };
    c.products[0]!.asset = { assetId: id };
    c.products[0]!.priceRub = 0;
    c.products[1]!.priceRub = null;
    delete c.products[2]!.priceRub;
    expect(parseCatalog(page(c))).toEqual(c);
    expect(priceLabel(0)).toMatch(/^0/);
    expect(priceLabel(3200)).toMatch(/^3\s200\s₽$/);
    expect(priceLabel()).toBe("Цена не указана");
    expect(priceLabel(null)).toBe("Цена не указана");
  });
  it("принимает пустую подборку и верхнюю границу количества", () => {
    expect(parseCatalog(page({ ...catalog, products: [] })).products).toEqual([]);
    const products = Array.from({ length: 64 }, (_, index) => ({
      ...catalog.products[0]!,
      id: `p_${index}`,
    }));
    expect(parseCatalog(page({ ...catalog, products })).products).toHaveLength(64);
  });
  it.each([null, [], false, 0, "config"])("отклоняет config другого типа: %j", (value) =>
    expect(() => parseCatalog(page(value))).toThrow("STORE_CONFIG_INVALID"),
  );
  it.each([
    "brand",
    "title",
    "subtitle",
    "description",
    "ctaLabel",
    "collectionTitle",
    "aboutTitle",
    "aboutText",
  ])("проверяет обязательный текст %s", (field) => {
    for (const value of ["", " ", null, 42, "x".repeat(2001)])
      expect(() => parseCatalog(page({ ...catalog, [field]: value }))).toThrow();
  });
  it.each([undefined, null, {}, "list"])("проверяет тип списка %j", (products) =>
    expect(() => parseCatalog(page({ ...catalog, products }))).toThrow(),
  );
  it("отклоняет 65 предметов, повтор ключа, неверный ключ и отсутствующие поля", () => {
    const p = catalog.products[0]!;
    for (const products of [
      Array.from({ length: 65 }, (_, i) => ({ ...p, id: `p${i}` })),
      [p, p],
      [{ ...p, id: "bad key" }],
      [{ ...p, id: "x".repeat(65) }],
      [{ ...p, title: "" }],
      [{ ...p, description: "" }],
      [{ ...p, material: "" }],
      [{ ...p, category: "" }],
      [{ ...p, tone: "unsafe" }],
    ])
      expect(() => parseCatalog(page({ ...catalog, products }))).toThrow();
  });
  it.each([-1, 1_000_000_001, 0.5, "0", NaN, Infinity])(
    "отклоняет недопустимую цену %j",
    (priceRub) =>
      expect(() =>
        parseCatalog(page({ ...catalog, products: [{ ...catalog.products[0]!, priceRub }] })),
      ).toThrow(),
  );
  it("принимает верхнюю границу цены", () =>
    expect(
      parseCatalog(
        page({ ...catalog, products: [{ ...catalog.products[0]!, priceRub: 1_000_000_000 }] }),
      ).products[0]!.priceRub,
    ).toBe(1_000_000_000));
  it.each([
    null,
    {},
    [],
    { assetId: 1 },
    { assetId: "javascript:alert(1)" },
    { assetId: id.toUpperCase() },
  ])("отклоняет неверную ссылку на ассет %j", (heroAsset) =>
    expect(() => parseCatalog(page({ ...catalog, heroAsset }))).toThrow(),
  );
  it("отклоняет другой корневой компонент и отсутствующий корень", () => {
    const p = page();
    p.document.nodes.n_store!.type = "Box";
    expect(() => parseCatalog(p)).toThrow();
    delete p.document.nodes.n_store;
    expect(() => parseCatalog(p)).toThrow();
  });
  it("не принимает обычный object без base", () => {
    const p = page();
    p.document.nodes.n_store!.props = { config: catalog };
    expect(() => parseCatalog(p)).toThrow();
  });
  it("отклоняет неподдерживаемые breakpoint overrides вместо их игнорирования", () => {
    const p = page();
    p.document.nodes.n_store!.props = { config: { base: catalog, sm: catalog } };
    expect(() => parseCatalog(p)).toThrow("STORE_CONFIG_INVALID");
  });
  it("обрабатывает произвольные JSON-входы контролируемым отказом", () => {
    fc.assert(
      fc.property(fc.jsonValue(), (value) => {
        try {
          parseCatalog(page(value));
          return false;
        } catch (error) {
          return error instanceof Error && error.message === "STORE_CONFIG_INVALID";
        }
      }),
    );
  });
  it("не даёт контенту закрыть JSON script и сохраняет строки без потерь", () => {
    const m = model();
    m.catalog!.title = "</script><script>alert(1)</script>\u2028\u2029";
    const serialized = serializeModel(m);
    expect(serialized).not.toContain("<");
    expect(serialized).not.toContain("\u2028");
    expect(JSON.parse(serialized)).toEqual(m);
  });
});
