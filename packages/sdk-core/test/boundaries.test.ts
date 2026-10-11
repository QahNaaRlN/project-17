import { afterEach, expect, it, vi } from "vitest";
import { createCmsClient, CmsError, type ClientOptions, type Preview } from "../src/index.js";
import * as decode from "../src/decode.js";
import { config, page, model, doc, routes, ID, json } from "./fixtures.js";
afterEach(() => vi.useRealTimers());

it("SDK-001: обе части credentials и края slug запрещены", () => {
  for (const baseUrl of ["https://user@a.test", "https://:pass@a.test"])
    expect(() => createCmsClient({ ...config, baseUrl })).toThrow(CmsError);
  for (const project of ["store!", "a".repeat(64)])
    expect(() => createCmsClient({ ...config, project })).toThrow(CmsError);
  expect(() =>
    createCmsClient({ ...config, project: "a".repeat(63), timeoutMs: 2147483647 }),
  ).not.toThrow();
});
it("API-040: preview неизвестного типа отклоняется до сети", async () => {
  const fetch = vi.fn<typeof globalThis.fetch>();
  const cms = createCmsClient({ ...config, fetch });
  for (const preview of [null, [], "token"])
    await expect(
      cms.getPage("/", { preview: preview as unknown as Preview }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
  expect(fetch).not.toHaveBeenCalled();
});
it("API-033: RFC problem JSON, MIME whitespace и отсутствующий MIME", async () => {
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(
      json({ code: "FORBIDDEN" }, { "Content-Type": "application/problem+json" }, 403),
    )
    .mockResolvedValueOnce(json(page, { "Content-Type": "application/json ; charset=utf-8" }))
    .mockResolvedValueOnce(new Response(new TextEncoder().encode(JSON.stringify(page))));
  const cms = createCmsClient({ ...config, fetch, cache: false });
  await expect(cms.getPage("/")).rejects.toMatchObject({ code: "FORBIDDEN", status: 403 });
  expect(await cms.getPage("/")).toEqual(page);
  await expect(cms.getPage("/")).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
});
it("API-033: серверный код проверяется целиком и status не маскируется", async () => {
  const fetch = vi.fn<typeof globalThis.fetch>();
  const cms = createCmsClient({ ...config, fetch });
  for (const code of ["xNOT_FOUND", "NOT_FOUNDx", 1, "A".repeat(65)]) {
    fetch.mockResolvedValueOnce(json({ code }, {}, 404));
    await expect(cms.getPage("/")).rejects.toMatchObject({ code: "HTTP_ERROR", status: 404 });
  }
  fetch.mockResolvedValueOnce(json({ code: "NOT_FOUND" }, {}, 500));
  await expect(cms.getPage("/")).rejects.toMatchObject({ code: "NOT_FOUND", status: 500 });
  expect(new CmsError("X").diagnostics).toEqual([]);
});
it("SDK transport: Content-Length ограничивает короткое тело независимо от stream", async () => {
  const bytes = new TextEncoder().encode(JSON.stringify(page)).length;
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(json(page, { "Content-Length": String(bytes + 1) }));
  await expect(
    createCmsClient({ ...config, fetch, maxResponseBytes: bytes }).getPage("/"),
  ).rejects.toMatchObject({ code: "RESPONSE_TOO_LARGE" });
});
it("SDK transport: timeout во время чтения и отмена после ответа", async () => {
  vi.useFakeTimers();
  const controller = new AbortController();
  const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(() =>
    Promise.resolve(
      new Response(
        new ReadableStream({
          start(c) {
            setTimeout(() => {
              c.enqueue(new TextEncoder().encode(JSON.stringify(page)));
              c.close();
            }, 11);
          },
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    ),
  );
  const cms = createCmsClient({ ...config, fetch, timeoutMs: 10 });
  const request = cms.getPage("/");
  const result = expect(request).rejects.toMatchObject({ code: "TIMEOUT" });
  await vi.advanceTimersByTimeAsync(11);
  await result;
  fetch.mockImplementation(async () => {
    controller.abort();
    return json(page);
  });
  await expect(cms.getPage("/", { signal: controller.signal })).rejects.toMatchObject({
    code: "ABORTED",
  });
});
it("SDK-020: no-store 200 и 404 удаляют старый ETag", async () => {
  for (const response of [
    json(page, { "Cache-Control": "no-store" }),
    json({ code: "NOT_FOUND" }, {}, 404),
  ]) {
    let now = 0;
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(json(page, { ETag: '"old"' }))
      .mockResolvedValueOnce(response)
      .mockResolvedValueOnce(json(page));
    const cms = createCmsClient({ ...config, fetch, now: () => now, cache: { ttlMs: 1 } });
    await cms.getPage("/");
    now = 1;
    await cms.getPage("/");
    await cms.getPage("/");
    expect(new Headers(fetch.mock.calls[2]![1]!.headers).has("If-None-Match")).toBe(false);
  }
});
it("SDK-002: непустые route params и все диагностики проверяются", () => {
  const diagnostic = {
    code: "NODE_ORPHAN",
    severity: "error",
    pointer: "/nodes/n_x",
    message: "x",
  };
  expect(
    decode.page({
      ...page,
      page: { ...page.page, path: "/a", params: { id: "abc" } },
      diagnostics: [diagnostic],
    }).page.params,
  ).toEqual({ id: "abc" });
  expect(decode.routes({ items: [] })).toEqual({ items: [] });
  expect(decode.routes({ items: [{ ...routes.items[0], path: "/a" }] }).items[0]!.path).toBe("/a");
  for (const patch of [{ code: 1 }, { pointer: 1 }, { severity: "other" }, { message: 1 }])
    expect(() =>
      decode.page({ ...page, diagnostics: [diagnostic, { ...diagnostic, ...patch }] }),
    ).toThrow(CmsError);
  expect(() =>
    decode.routes({ items: [routes.items[0], { ...routes.items[0], path: 1 }] }),
  ).toThrow(CmsError);
  expect(() => decode.page({ ...page, page: { ...page.page, params: { a: "ok", b: 1 } } })).toThrow(
    CmsError,
  );
  expect(() =>
    decode.document({ ...doc, diagnostics: [diagnostic, { ...diagnostic, message: 1 }] }),
  ).toThrow(CmsError);
});
it("SDK-002: UUID проверяется целиком", () => {
  for (const id of [ID + "x", "x" + ID])
    expect(() => decode.document({ ...doc, objectId: id })).toThrow(CmsError);
  expect(() => decode.document({ ...doc, objectId: { toString: () => ID } })).toThrow(CmsError);
});
it("CNT-052: http допустим, credentials и дополнительные пути запрещены", () => {
  const url = model.variants[0]!.url;
  const path = new URL(url).pathname;
  expect(
    decode.image({ ...model, variants: [{ width: 320, url: "http://a.test" + path }] }, 0)
      .variants[0]!.url,
  ).toBe("http://a.test" + path);
  for (const bad of [
    "ftp://a.test" + path,
    "https://user@a.test" + path,
    "https://:pass@a.test" + path,
    "https://a.test/prefix" + path,
    "https://a.test" + path + "/suffix",
  ])
    expect(() => decode.image({ ...model, variants: [{ width: 320, url: bad }] }, 0)).toThrow(
      CmsError,
    );
  expect(() => decode.image({ ...model, focalPoint: { x: "0.5", y: 0 } }, 0)).toThrow(CmsError);
  expect(
    decode.image({ ...model, variants: decode.widths.map((width) => ({ width, url })) }, 0)
      .variants,
  ).toHaveLength(9);
});
it("CNT-052: getImage возвращает модель, а не только завершает запрос", async () => {
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValueOnce(json(model));
  expect(await createCmsClient({ ...config, fetch, now: () => 0 }).getImage(ID)).toEqual(model);
});
it("SDK-001: аргументы нестандартного типа не уходят в сеть", () => {
  const cms = createCmsClient(config);
  expect(() => cms.getPage(1 as unknown as string)).toThrow(CmsError);
  expect(() => cms.getDocument(new String(ID) as unknown as string)).toThrow(CmsError);
  expect(() => createCmsClient({ ...config, deliveryKey: 1 as unknown as string })).toThrow(
    CmsError,
  );
  expect(() => createCmsClient({ ...config, project: 1 as unknown as string })).toThrow(CmsError);
  expect(() => createCmsClient({ ...config, cache: { capacity: NaN } } as ClientOptions)).toThrow(
    CmsError,
  );
});
