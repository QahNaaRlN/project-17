import { afterEach, expect, it, vi } from "vitest";
import { createCmsClient, CmsError } from "../src/index.js";
import * as decode from "../src/decode.js";
import { config, page, model, ID, ir, json } from "./fixtures.js";
afterEach(() => vi.useRealTimers());

it("SDK-020: preview head и его ошибки сохраняют тёплый published кэш", async () => {
  for (const response of [
    json(page),
    json({ code: "NOT_FOUND" }, {}, 404),
    json({ code: "FORBIDDEN" }, {}, 403),
  ]) {
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(json(page))
      .mockResolvedValueOnce(response);
    const cms = createCmsClient({ ...config, fetch });
    await cms.getPage("/");
    try {
      await cms.getPage("/", { preview: { token: "head" } });
    } catch (error) {
      expect(error).toMatchObject({ code: "FORBIDDEN" });
    }
    expect(await cms.getPage("/")).toEqual(page);
    expect(fetch).toHaveBeenCalledTimes(2);
  }
});
it("SDK-020: ошибка старого запроса после clearCache не удаляет новый ответ", async () => {
  for (const status of [404, 503]) {
    const fetch = vi.fn<typeof globalThis.fetch>();
    let resolve!: (response: Response) => void;
    fetch
      .mockReturnValueOnce(
        new Promise((r) => {
          resolve = r;
        }),
      )
      .mockResolvedValueOnce(json(page));
    const cms = createCmsClient({ ...config, fetch });
    const old = cms.getPage("/");
    cms.clearCache();
    await cms.getPage("/");
    resolve(json({ code: "NOT_FOUND" }, {}, status));
    try {
      await old;
    } catch (error) {
      expect(error).toMatchObject({ status: 503 });
    }
    expect(await cms.getPage("/")).toEqual(page);
    expect(fetch).toHaveBeenCalledTimes(2);
  }
});
it("SDK-020: завершённая revalidation становится последней использованной записью", async () => {
  let now = 0;
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockImplementation(async () => json(page, { ETag: '"v"' }));
  const cms = createCmsClient({
    ...config,
    fetch,
    now: () => now,
    cache: { capacity: 2, ttlMs: 1 },
  });
  await cms.getPage("/a");
  await cms.getPage("/b");
  now = 1;
  let resolve!: (response: Response) => void;
  fetch.mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  const old = cms.getPage("/a");
  await cms.getPage("/b");
  resolve(new Response(null, { status: 304 }));
  await old;
  await cms.getPage("/c");
  await cms.getPage("/a");
  expect(fetch).toHaveBeenCalledTimes(5);
});
it("SDK-020: no-store с пробелом перед запятой запрещает сохранение", async () => {
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockImplementation(async () => json(page, { "Cache-Control": "no-store , max-age=1" }));
  const cms = createCmsClient({ ...config, fetch });
  await cms.getPage("/");
  await cms.getPage("/");
  expect(fetch).toHaveBeenCalledTimes(2);
});
it("SDK transport: reader освобождается и успешный запрос не оставляет timer", async () => {
  vi.useFakeTimers();
  const response = json(page);
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValueOnce(response);
  await createCmsClient({ ...config, fetch }).getPage("/");
  expect(response.body!.locked).toBe(false);
  expect(vi.getTimerCount()).toBe(0);
});
it("SDK-001: malformed URL сохраняет код INVALID_ARGUMENT", () => {
  try {
    createCmsClient({ ...config, baseUrl: "invalid" });
    expect.fail();
  } catch (error) {
    expect(error).toMatchObject({ code: "INVALID_ARGUMENT", status: 0 });
  }
});
it("API-033: массив не является машинным кодом ошибки", async () => {
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(json({ code: ["NOT_FOUND"] }, {}, 404));
  await expect(createCmsClient({ ...config, fetch }).getPage("/")).rejects.toMatchObject({
    code: "HTTP_ERROR",
    status: 404,
  });
});
it("CNT-052: незаданные image параметры отсутствуют в URL и malformed URL даёт INVALID_RESPONSE", async () => {
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(json(model))
    .mockResolvedValueOnce(json({ ...model, variants: [{ width: 320, url: "invalid" }] }));
  const cms = createCmsClient({ ...config, fetch, now: () => 0 });
  await cms.getImage(ID);
  expect(fetch.mock.calls[0]![0]).toBe(
    `https://cms.test/delivery/v1/store/staging/asset/${ID}/image`,
  );
  await expect(cms.getImage(ID)).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
});
it("SDK-002: component version, page path и url должны иметь правильные типы", () => {
  expect(() => decode.page({ ...page, page: { ...page.page, path: 1 } })).toThrow(CmsError);
  expect(() =>
    decode.page({
      ...page,
      components: { [ID]: { versionId: "invalid", document: { ...ir, kind: "component" } } },
    }),
  ).toThrow();
  expect(() =>
    decode.image({ ...model, variants: [{ width: 320, url: [model.variants[0]!.url] }] }, 0),
  ).toThrow();
  expect(decode.image({ ...model, fit: "cover" }, 0).fit).toBe("cover");
});
