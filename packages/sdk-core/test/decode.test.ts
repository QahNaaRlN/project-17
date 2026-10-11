import { describe, expect, it } from "vitest";
import fc from "fast-check";
import * as decode from "../src/decode.js";
import { CmsError } from "../src/index.js";
import { page, doc, asset, routes, model, ir, ID } from "./fixtures.js";

describe("SDK-002: проверка контрактов Delivery", () => {
  it("принимает страницу, Composed документы и серверные диагностики", () => {
    const value = {
      ...page,
      components: { [ID]: { versionId: ID, document: { ...ir, kind: "component" } } },
      diagnostics: [{ code: "WARNING", severity: "warning", pointer: "/", message: "warning" }],
    };
    expect(decode.page(value)).toEqual(value);
    expect(decode.document({ ...doc, path: "/a" })).toEqual({ ...doc, path: "/a" });
    expect(decode.content({ ...asset, schema: "Image", schemaVersion: 1 }, "asset").schema).toBe(
      "Image",
    );
    expect(decode.routes(routes)).toEqual(routes);
    expect(decode.image(model, 0)).toEqual(model);
  });
  it.each(["2.0", "0.9"])("не поддерживает IR %s и сохраняет диагностику", (version) => {
    try {
      decode.page({ ...page, document: { ...ir, irVersion: version } });
      expect.fail();
    } catch (error) {
      expect(error).toMatchObject({
        code: "IR_INVALID",
        diagnostics: [
          expect.objectContaining({ code: "IR_VERSION_UNSUPPORTED", pointer: "/irVersion" }),
        ],
      });
    }
  });
  it("отклоняет повреждённый IR и Composed, сохраняя node/pointer", () => {
    expect(() => decode.document({ ...doc, document: { ...ir, root: "n_missing" } })).toThrow(
      CmsError,
    );
    expect(() => decode.page({ ...page, components: { card: null } })).toThrow(CmsError);
    expect(() => decode.page({ ...page, document: { ...ir, kind: "component" } })).toThrow(
      CmsError,
    );
    expect(() =>
      decode.page({ ...page, components: { [ID]: { versionId: "bad", document: ir } } }),
    ).toThrow(CmsError);
    expect(() =>
      decode.page({ ...page, components: { [ID]: { versionId: ID, document: ir } } }),
    ).toThrow(CmsError);
    expect(() =>
      decode.page({ ...page, components: { [ID]: { versionId: ID, document: null } } }),
    ).toThrow(CmsError);
  });
  it.each([
    null,
    [],
    {},
    { ...page, page: null },
    { ...page, page: { ...page.page, objectId: "bad" } },
    { ...page, page: { ...page.page, versionId: "bad" } },
    { ...page, page: { ...page.page, path: "bad" } },
    { ...page, page: { ...page.page, params: [] } },
    { ...page, page: { ...page.page, params: { id: 1 } } },
    { ...page, components: [] },
    { ...page, data: [] },
    { ...page, dataSources: null },
    { ...page, diagnostics: {} },
    { ...page, diagnostics: [null] },
    { ...page, diagnostics: [{ code: 1 }] },
    { ...page, diagnostics: [{ code: "X", pointer: 1 }] },
    { ...page, diagnostics: [{ code: "X", pointer: "/", severity: "other" }] },
    { ...page, diagnostics: [{ code: "X", pointer: "/", severity: "error", message: 1 }] },
  ])("не принимает повреждённую страницу %#", (value) =>
    expect(() => decode.page(value)).toThrow(CmsError),
  );
  it.each([
    null,
    { ...doc, objectId: "x" },
    { ...doc, versionId: "x" },
    { ...doc, path: 1 },
    { ...doc, path: "x" },
  ])("проверяет envelope документа %#", (value) =>
    expect(() => decode.document(value)).toThrow(CmsError),
  );
  it.each([
    null,
    { ...asset, id: "x" },
    { ...asset, versionId: "x" },
    { ...asset, kind: "entity" },
    { ...asset, deleted: 1 },
    { ...asset, data: [] },
    { ...asset, schema: 1 },
    { ...asset, schemaVersion: 0 },
    { ...asset, schemaVersion: 1.5 },
  ])("проверяет envelope контента %#", (value) =>
    expect(() => decode.content(value, "asset")).toThrow(CmsError),
  );
  it.each([
    null,
    { items: null },
    { items: [null] },
    { items: [{ ...routes.items[0], objectId: "x" }] },
    { items: [{ ...routes.items[0], versionId: "x" }] },
    { items: [{ ...routes.items[0], path: "x" }] },
  ])("проверяет таблицу маршрутов %#", (value) =>
    expect(() => decode.routes(value)).toThrow(CmsError),
  );
  it.each([
    null,
    { ...model, assetId: "x" },
    { ...model, versionId: "x" },
    { ...model, width: 0 },
    { ...model, height: -1 },
    { ...model, width: 10000, height: 4001 },
    { ...model, fit: "x" },
    { ...model, format: "svg" },
    { ...model, focalPoint: null },
    { ...model, focalPoint: { x: NaN, y: 0 } },
    { ...model, focalPoint: { x: 0, y: 1.1 } },
    { ...model, focalPoint: { x: -0.1, y: 0 } },
    { ...model, expiresAt: "bad" },
    { ...model, expiresAt: "1970-01-01T00:00:00Z" },
    { ...model, expiresAt: 1 },
    { ...model, variants: [] },
    { ...model, variants: Array(10).fill(model.variants[0]) },
    { ...model, variants: [null] },
    { ...model, variants: [{ width: 319, url: model.variants[0]!.url }] },
    { ...model, variants: [model.variants[0], model.variants[0]] },
    { ...model, variants: [{ width: 320, url: 1 }] },
    ...[
      "invalid",
      "javascript:alert(1)",
      "https://a.test/wrong",
      `https://user:pass@a.test/assets/v1/staging/${ID}/${"a".repeat(64)}`,
      `${model.variants[0]!.url}#x`,
      `${model.variants[0]!.url},`,
      `${model.variants[0]!.url} `,
    ].map((url) => ({ ...model, variants: [{ width: 320, url }] })),
  ])("проверяет image model и URLs %#", (value) =>
    expect(() => decode.image(value, 0)).toThrow(CmsError),
  );
  it("проверяет обе границы focalPoint, expiry и допустимые ширины", () => {
    expect(
      decode.image({ ...model, width: 10000, height: 4000, focalPoint: { x: 0, y: 1 } }, 0).width,
    ).toBe(10000);
    expect(() => decode.image(model, Date.parse(model.expiresAt))).toThrow(CmsError);
    for (const width of decode.widths)
      expect(
        decode.image({ ...model, variants: [{ ...model.variants[0], width }] }, 0).variants[0]!
          .width,
      ).toBe(width);
  });
  it("парсеры на произвольном JSON возвращают типизированные ошибки", () => {
    fc.assert(
      fc.property(fc.jsonValue(), (value) => {
        for (const parse of [
          decode.page,
          decode.document,
          decode.routes,
          (v: unknown) => decode.content(v, "asset"),
          (v: unknown) => decode.image(v, 0),
        ]) {
          try {
            parse(value);
          } catch (error) {
            expect(error).toBeInstanceOf(CmsError);
          }
        }
      }),
      { numRuns: 100 },
    );
  });
});
