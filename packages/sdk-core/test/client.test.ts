import { afterEach, describe, expect, it, vi } from "vitest";
import { CmsError, createCmsClient, type ClientOptions } from "../src/index.js";
import { config, ID, page, doc, asset, routes, model, json } from "./fixtures.js";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
function setup(options: Partial<ClientOptions> = {}) {
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(json(page));
  let clock = 0;
  const client = createCmsClient({ ...config, fetch, now: () => clock, ...options });
  return {
    client,
    fetch,
    at: (value: number) => {
      clock = value;
    },
  };
}
function init(fetch: ReturnType<typeof setup>["fetch"], n = 0) {
  return fetch.mock.calls[n]![1]!;
}
function header(fetch: ReturnType<typeof setup>["fetch"], name: string, n = 0) {
  return new Headers(init(fetch, n).headers).get(name);
}
describe("SDK-001 / API-033: клиент чтения", () => {
  it("кодирует путь, отправляет Bearer и не передаёт cookies/redirect/cache", async () => {
    const s = setup();
    expect(await s.client.getPage("/a?x=1&token=evil")).toEqual(page);
    expect(s.fetch.mock.calls[0]![0]).toBe(
      "https://cms.test/delivery/v1/store/staging/page?path=%2Fa%3Fx%3D1%26token%3Devil",
    );
    expect(header(s.fetch, "Authorization")).toBe("Bearer cms_pub_secret");
    expect(init(s.fetch)).toMatchObject({
      method: "GET",
      credentials: "omit",
      redirect: "error",
      cache: "no-store",
    });
    expect(header(s.fetch, "Accept")).toBe("application/json");
  });
  it("поддерживает документы, контент и маршруты", async () => {
    const s = setup({ cache: false });
    s.fetch
      .mockResolvedValueOnce(json(doc))
      .mockResolvedValueOnce(
        json({ ...asset, kind: "entity", schema: "Product", schemaVersion: 1 }),
      )
      .mockResolvedValueOnce(json(asset))
      .mockResolvedValueOnce(json(routes));
    expect(await s.client.getDocument(ID.toUpperCase())).toEqual(doc);
    expect((await s.client.getEntity(ID))?.schema).toBe("Product");
    expect(await s.client.getAsset(ID)).toEqual(asset);
    expect(await s.client.getRoutes()).toEqual(routes);
    expect(s.fetch.mock.calls.map((call) => String(call[0]).split("/").at(-1))).toEqual([
      ID,
      ID,
      ID,
      "routes",
    ]);
  });
  it.each([400, 401, 403, 409, 422, 429, 500, 503])(
    "сохраняет машинный код и статус %i без секретов сервера",
    async (status) => {
      const s = setup();
      s.fetch.mockResolvedValue(
        json({ code: "POLICY_DENIED", detail: config.deliveryKey, traceId: "trace" }, {}, status),
      );
      await expect(s.client.getPage("/")).rejects.toMatchObject({
        name: "CmsError",
        code: "POLICY_DENIED",
        status,
      });
      try {
        await s.client.getPage("/");
      } catch (error) {
        expect(JSON.stringify(error)).not.toContain(config.deliveryKey);
        expect(String(error)).not.toContain(config.deliveryKey);
      }
    },
  );
  it("возвращает null только на контрактный 404 NOT_FOUND", async () => {
    const s = setup();
    s.fetch.mockImplementation(async () => json({ code: "NOT_FOUND" }, {}, 404));
    expect(await s.client.getPage("/")).toBeNull();
    expect(await s.client.getDocument(ID)).toBeNull();
    expect(await s.client.getEntity(ID)).toBeNull();
    expect(await s.client.getAsset(ID)).toBeNull();
    expect(await s.client.getImage(ID)).toBeNull();
    await expect(s.client.getRoutes()).rejects.toMatchObject({ code: "NOT_FOUND", status: 404 });
    s.fetch.mockResolvedValue(json({ code: "bad secret" }, {}, 404));
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "HTTP_ERROR", status: 404 });
    s.fetch.mockResolvedValue(new Response("proxy HTML", { status: 502 }));
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "HTTP_ERROR", status: 502 });
  });
  it("пропускает отсутствующий код и не доверяет 304 без cached body", async () => {
    const s = setup();
    s.fetch
      .mockResolvedValueOnce(json({}, {}, 500))
      .mockResolvedValueOnce(new Response(null, { status: 304 }));
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "HTTP_ERROR", status: 500 });
    await expect(s.client.getPage("/")).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      status: 304,
    });
  });
  it.each([
    { baseUrl: "bad" },
    { baseUrl: "file:///x" },
    { baseUrl: "https://a.test/base" },
    { baseUrl: "https://u:p@a.test" },
    { baseUrl: "https://a.test/?x=1" },
    { baseUrl: "https://a.test/#x" },
    { project: "../evil" },
    { environment: "UPPER" },
    { deliveryKey: "bad\nsecret" },
    { timeoutMs: 0 },
    { timeoutMs: 2147483648 },
    { maxResponseBytes: 0 },
    { cache: { capacity: -1 } },
    { cache: { capacity: 1.5 } },
    { cache: { ttlMs: 0 } },
  ])("проверяет конфигурацию %#", (options) => expect(() => setup(options)).toThrow(CmsError));
  it("не требует published ключ для preview-only клиента", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(json(page));
    const client = createCmsClient({
      baseUrl: config.baseUrl,
      project: "store",
      environment: "staging",
      fetch,
    });
    await expect(client.getPage("/")).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(await client.getPage("/", { preview: { token: "preview" } })).toEqual(page);
    vi.stubGlobal("fetch", undefined);
    expect(() => createCmsClient(config)).toThrow(CmsError);
  });
  it("использует стандартный fetch и clock", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(json(page));
    vi.stubGlobal("fetch", fetch);
    expect(await createCmsClient(config).getPage("/")).toEqual(page);
    expect(fetch).toHaveBeenCalledOnce();
  });
  it("отклоняет аргументы до сетевого запроса", () => {
    const s = setup();
    for (const path of ["x", "/" + "a".repeat(2048), "/" + "я".repeat(1024)])
      expect(() => s.client.getPage(path)).toThrow(CmsError);
    expect(() => s.client.getDocument("bad")).toThrow(CmsError);
    expect(() => s.client.getAsset("../x")).toThrow(CmsError);
    expect(() => s.client.getImage(ID, { fit: "cover" })).toThrow(CmsError);
    for (const h of [-1, 2561, 1.5])
      expect(() => s.client.getImage(ID, { fit: "cover", h })).toThrow(CmsError);
    expect(() => s.client.getImage(ID, { h: 1 })).toThrow(CmsError);
    expect(() => s.client.getImage(ID, { w: 319 })).toThrow(CmsError);
    expect(() => s.client.getImage(ID, { fmt: "svg" as "png" })).toThrow(CmsError);
    expect(() => s.client.getImage(ID, { fit: "stretch" as "cover" })).toThrow(CmsError);
    expect(s.fetch).not.toHaveBeenCalled();
  });
  it("допускает граничную длину пути и валидные image options", async () => {
    const s = setup({ cache: false });
    await s.client.getPage("/" + "a".repeat(2047));
    s.fetch.mockImplementation(async () => json(model));
    await s.client.getImage(ID, { fmt: "webp", fit: "cover", w: 320, h: 2560 });
    await s.client.getImage(ID, { fit: "contain", h: 0 });
    await s.client.getImage(ID, { fit: "cover", h: 1 });
    expect(s.fetch.mock.calls[1]![0]).toBe(
      `https://cms.test/delivery/v1/store/staging/asset/${ID}/image?fmt=webp&fit=cover&w=320&h=2560`,
    );
  });
});

describe("SDK-020: published LRU и ETag", () => {
  it("возвращает копии до TTL и revalidate ровно на границе", async () => {
    const s = setup();
    s.fetch
      .mockResolvedValueOnce(json(page, { ETag: '"v1"' }))
      .mockResolvedValueOnce(new Response(null, { status: 304, headers: { ETag: '"v1"' } }));
    const first = (await s.client.getPage("/"))!;
    first.data.mutated = true;
    s.at(59999);
    expect((await s.client.getPage("/"))!.data).toEqual({});
    expect(s.fetch).toHaveBeenCalledOnce();
    s.at(60000);
    expect(await s.client.getPage("/")).toEqual(page);
    expect(header(s.fetch, "If-None-Match", 1)).toBe('"v1"');
    s.at(119999);
    await s.client.getPage("/");
    expect(s.fetch).toHaveBeenCalledTimes(2);
  });
  it("обновляет ответ после изменения ETag и не меняет cache через возвращаемую копию", async () => {
    const s = setup({ cache: { ttlMs: 10 } });
    s.fetch
      .mockResolvedValueOnce(json(page, { ETag: '"a"' }))
      .mockResolvedValueOnce(json({ ...page, data: { new: 1 } }, { ETag: '"b"' }));
    await s.client.getPage("/");
    s.at(10);
    expect((await s.client.getPage("/"))!.data).toEqual({ new: 1 });
    const hit = (await s.client.getPage("/"))!;
    hit.document.root = "bad";
    expect((await s.client.getPage("/"))!.document.root).toBe("n_root");
  });
  it("вытесняет least recently used и clearCache удаляет ETag", async () => {
    const s = setup({ cache: { capacity: 2 } });
    s.fetch.mockImplementation(async () => json(page, { ETag: '"v"' }));
    await s.client.getPage("/a");
    await s.client.getPage("/b");
    await s.client.getPage("/a");
    await s.client.getPage("/c");
    await s.client.getPage("/b");
    expect(s.fetch).toHaveBeenCalledTimes(4);
    s.client.clearCache();
    await s.client.getPage("/a");
    expect(header(s.fetch, "If-None-Match", 4)).toBeNull();
  });
  it("clearCache предотвращает восстановление записей от in-flight запроса", async () => {
    const s = setup();
    let resolve!: (r: Response) => void;
    s.fetch.mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const request = s.client.getPage("/");
    s.client.clearCache();
    resolve(json(page));
    await request;
    await s.client.getPage("/");
    expect(s.fetch).toHaveBeenCalledTimes(2);
  });
  it("clearCache во время 304 не восстанавливает старую запись", async () => {
    const s = setup({ cache: { ttlMs: 1 } });
    s.fetch.mockResolvedValueOnce(json(page, { ETag: '"v"' }));
    await s.client.getPage("/");
    s.at(1);
    let resolve!: (r: Response) => void;
    s.fetch.mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const request = s.client.getPage("/");
    s.client.clearCache();
    resolve(new Response(null, { status: 304 }));
    await request;
    await s.client.getPage("/");
    expect(header(s.fetch, "If-None-Match", 2)).toBeNull();
  });
  it.each(["no-store", "private", "public, no-store", 'PRIVATE="Authorization"'])(
    "уважает %s и не кэширует",
    async (cc) => {
      const s = setup();
      s.fetch.mockImplementation(async () => json(page, { "Cache-Control": cc }));
      await s.client.getPage("/");
      await s.client.getPage("/");
      expect(s.fetch).toHaveBeenCalledTimes(2);
    },
  );
  it("304 no-store удаляет старое тело из кэша", async () => {
    const s = setup({ cache: { ttlMs: 1 } });
    s.fetch
      .mockResolvedValueOnce(json(page, { ETag: '"v"' }))
      .mockResolvedValueOnce(
        new Response(null, { status: 304, headers: { "Cache-Control": "no-store" } }),
      );
    await s.client.getPage("/");
    s.at(1);
    await s.client.getPage("/");
    await s.client.getPage("/");
    expect(header(s.fetch, "If-None-Match", 2)).toBeNull();
  });
  it.each([false as const, { capacity: 0 }])("отключает кэш %#", async (cache) => {
    const s = setup({ cache });
    s.fetch.mockImplementation(async () => json(page));
    await s.client.getPage("/");
    await s.client.getPage("/");
    expect(s.fetch).toHaveBeenCalledTimes(2);
  });
  it("не возвращает stale после ошибки и удаляет ETag", async () => {
    const s = setup({ cache: { ttlMs: 1 } });
    s.fetch
      .mockResolvedValueOnce(json(page, { ETag: '"v"' }))
      .mockResolvedValueOnce(json({ code: "UNAUTHENTICATED" }, {}, 401));
    await s.client.getPage("/");
    s.at(1);
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "UNAUTHENTICATED" });
    await s.client.getPage("/");
    expect(header(s.fetch, "If-None-Match", 2)).toBeNull();
  });
  it("без ETag повторяет полный GET", async () => {
    const s = setup({ cache: { ttlMs: 1 } });
    s.fetch.mockImplementation(async () => json(page));
    await s.client.getPage("/");
    s.at(1);
    await s.client.getPage("/");
    expect(header(s.fetch, "If-None-Match", 1)).toBeNull();
  });
  it("клиенты для разных окружений/ключей не делят кэш", async () => {
    const a = setup(),
      b = setup({ environment: "production", deliveryKey: "other" });
    await a.client.getPage("/");
    await b.client.getPage("/");
    expect(a.fetch).toHaveBeenCalledOnce();
    expect(b.fetch).toHaveBeenCalledOnce();
    expect(header(b.fetch, "Authorization")).toBe("Bearer other");
  });
});

describe("API-040: preview и подписанные модели", () => {
  it("не читает и не заполняет published кэш, связывает Change Set с токеном", async () => {
    const s = setup();
    s.fetch.mockImplementation(async () => json(page, { ETag: '"v"' }));
    await s.client.getPage("/");
    for (let i = 0; i < 2; i++)
      await s.client.getPage("/", { preview: { token: "preview-secret", changesetId: ID } });
    await s.client.getPage("/");
    expect(s.fetch).toHaveBeenCalledTimes(3);
    expect(header(s.fetch, "Authorization", 1)).toBe("Preview preview-secret");
    expect(header(s.fetch, "If-None-Match", 1)).toBeNull();
    expect(String(s.fetch.mock.calls[1]![0])).toContain(`changesetId=${ID}`);
    expect(String(s.fetch.mock.calls[1]![0])).not.toContain("preview-secret");
  });
  it("preview head также всегда запрашивается заново", async () => {
    const s = setup();
    s.fetch.mockImplementation(async () => json(page));
    await s.client.getPage("/", { preview: { token: "head" } });
    await s.client.getPage("/", { preview: { token: "head" } });
    expect(s.fetch).toHaveBeenCalledTimes(2);
    expect(String(s.fetch.mock.calls[0]![0])).not.toContain("changesetId");
  });
  it("не кэширует image model даже с public/ETag", async () => {
    const s = setup();
    s.fetch.mockImplementation(async () => json(model, { ETag: '"v"', "Cache-Control": "public" }));
    await s.client.getImage(ID);
    await s.client.getImage(ID);
    expect(s.fetch).toHaveBeenCalledTimes(2);
    expect(header(s.fetch, "If-None-Match", 1)).toBeNull();
  });
  it("отклоняет malformed preview и Change Set без обращения к сети", async () => {
    const s = setup();
    await expect(s.client.getPage("/", { preview: { token: "" } })).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
    });
    await expect(s.client.getPage("/", { preview: { token: "a\r\nb" } })).rejects.toMatchObject({
      code: "INVALID_ARGUMENT",
    });
    await expect(
      s.client.getPage("/", { preview: { token: "x", changesetId: "bad" } }),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(s.fetch).not.toHaveBeenCalled();
  });
});

describe("SDK транспорт: границы и ошибки", () => {
  it.each([
    new Response("html", { headers: { "Content-Type": "text/html" } }),
    new Response("invalid", { headers: { "Content-Type": "application/json" } }),
    new Response(null, { headers: { "Content-Type": "application/json" } }),
    json({ wrong: true }),
  ])("не принимает невалидный ответ %#", async (response) => {
    const s = setup();
    s.fetch.mockResolvedValue(response);
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });
  it("поддерживает JSON MIME с параметрами и UTF-8", async () => {
    const s = setup();
    s.fetch.mockResolvedValue(
      json(
        { ...page, data: { text: "Привет" } },
        { "Content-Type": "Application/JSON; charset=utf-8" },
      ),
    );
    expect((await s.client.getPage("/"))!.data).toEqual({ text: "Привет" });
  });
  it("не принимает повреждённый UTF-8", async () => {
    const s = setup();
    s.fetch.mockResolvedValue(
      new Response(new Uint8Array([0xff]), { headers: { "Content-Type": "application/json" } }),
    );
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });
  it("ограничивает Content-Length и chunked ответы", async () => {
    const s = setup({ maxResponseBytes: 10 });
    s.fetch
      .mockResolvedValueOnce(json(page, { "Content-Length": "11" }))
      .mockResolvedValueOnce(json(page));
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "RESPONSE_TOO_LARGE" });
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "RESPONSE_TOO_LARGE" });
  });
  it("допускает точный размер и несколько chunks", async () => {
    const bytes = new TextEncoder().encode(JSON.stringify(page));
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(bytes.slice(0, 4));
        c.enqueue(bytes.slice(4));
        c.close();
      },
    });
    const s = setup({ maxResponseBytes: bytes.byteLength });
    s.fetch.mockResolvedValue(
      new Response(stream, {
        headers: { "Content-Type": "application/json", "Content-Length": String(bytes.byteLength) },
      }),
    );
    expect(await s.client.getPage("/")).toEqual(page);
  });
  it("сохраняет HTTP status на ответе ошибки с oversized body", async () => {
    const s = setup({ maxResponseBytes: 1 });
    s.fetch.mockResolvedValue(json({ code: "POLICY_DENIED" }, {}, 503));
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "HTTP_ERROR", status: 503 });
  });
  it("network/stream errors не содержат upstream сообщение", async () => {
    const s = setup();
    s.fetch.mockRejectedValueOnce(new Error(config.deliveryKey)).mockResolvedValueOnce(
      new Response(
        new ReadableStream({
          start(c) {
            c.error(new Error(config.deliveryKey));
          },
        }),
      ),
    );
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "NETWORK_ERROR" });
    await expect(s.client.getPage("/")).rejects.toMatchObject({ code: "NETWORK_ERROR" });
  });
  it("отклоняет уже отменённый запрос даже при тёплом кэше", async () => {
    const s = setup();
    await s.client.getPage("/");
    await expect(s.client.getPage("/", { signal: AbortSignal.abort() })).rejects.toMatchObject({
      code: "ABORTED",
    });
    expect(s.fetch).toHaveBeenCalledOnce();
  });
  it("прерывает Fetch по таймауту и очищает timer", async () => {
    vi.useFakeTimers();
    const s = setup({ timeoutMs: 10 });
    s.fetch.mockImplementation(
      (_url, options) =>
        new Promise((_resolve, reject) =>
          options!.signal!.addEventListener("abort", () => reject(new Error("abort"))),
        ),
    );
    const result = s.client.getPage("/");
    const assertion = expect(result).rejects.toMatchObject({ code: "TIMEOUT" });
    await vi.advanceTimersByTimeAsync(10);
    await assertion;
    expect(vi.getTimerCount()).toBe(0);
  });
  it("AbortSignal передаётся в fetch и подписка удаляется", async () => {
    const s = setup();
    const controller = new AbortController();
    const remove = vi.spyOn(controller.signal, "removeEventListener");
    s.fetch.mockImplementation(
      (_url, options) =>
        new Promise((_resolve, reject) =>
          options!.signal!.addEventListener("abort", () => reject(new Error("abort"))),
        ),
    );
    const result = s.client.getPage("/", { signal: controller.signal });
    controller.abort();
    await expect(result).rejects.toMatchObject({ code: "ABORTED" });
    expect(remove).toHaveBeenCalledWith("abort", expect.any(Function));
  });
  it("timeout/abort проверяется даже если custom fetch возвращает ответ", async () => {
    vi.useFakeTimers();
    const s = setup({ timeoutMs: 10 });
    s.fetch.mockImplementation(
      () => new Promise((resolve) => setTimeout(() => resolve(json(page)), 11)),
    );
    const result = s.client.getPage("/");
    const assertion = expect(result).rejects.toMatchObject({ code: "TIMEOUT" });
    await vi.advanceTimersByTimeAsync(11);
    await assertion;
  });
});
