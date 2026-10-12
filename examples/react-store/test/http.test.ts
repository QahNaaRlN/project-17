import type { IncomingMessage, ServerResponse } from "node:http";
import { describe, it, expect, vi } from "vitest";
import { documentHtml, storeHandler } from "../src/http.js";
import { model } from "./fixtures.js";
import type { StoreModel } from "../src/model.js";

// Test the handler contract directly. The desktop proxy rewrites HTML HEAD
// replies; actual HTTP/SSR/hydration is covered by the Playwright suite.
function response() {
  const headers = new Map<string, string>();
  let body = "";
  const value = {
    statusCode: 200,
    setHeader: (name: string, value: string) => headers.set(name.toLowerCase(), value),
    writeHead: (status: number, extra: Record<string, string> = {}) => {
      value.statusCode = status;
      for (const [k, v] of Object.entries(extra)) headers.set(k.toLowerCase(), v);
    },
    end: (data?: string) => {
      body = data ?? "";
    },
  };
  return { value: value as unknown as ServerResponse, headers, body: () => body };
}
describe("STORE-004/005/007: HTTP и SSR", () => {
  it("SDK-030: открывает CMS маршрут страницы после preview redirect", async () => {
    const res = response(),
      load = vi.fn(async () => model());
    await storeHandler(
      load,
      async () => ({ scripts: [], styles: [] }),
      undefined,
      undefined,
      undefined,
      "/store",
    )({ method: "GET", url: "/store" } as IncomingMessage, res.value);
    expect(res.value.statusCode).toBe(200);
    expect(load).toHaveBeenCalled();
  });
  it.each(["GET", "HEAD"])(
    "MF-005: exposes this application's built hash without loading CMS content: %s",
    async (method) => {
      const load = vi.fn();
      const hash = "sha256:" + "a".repeat(64);
      const res = response();
      await storeHandler(
        load,
        async () => ({ scripts: [], styles: [] }),
        undefined,
        hash,
      )({ method, url: "/.well-known/cms-manifest-hash" } as IncomingMessage, res.value);
      expect(res.value.statusCode).toBe(200);
      expect(res.body()).toBe(method === "GET" ? hash + "\n" : "");
      expect(load).not.toHaveBeenCalled();
      expect(res.headers.get("cache-control")).toBe("no-store");
    },
  );
  it("рендерит текст и безопасную модель до JavaScript", () => {
    const m = model();
    m.catalog!.title = "<script>unsafe</script>";
    const html = documentHtml(m, {
      scripts: ["/assets/client.js"],
      styles: ["/assets/client.css"],
    });
    expect(html).toContain("&lt;script&gt;unsafe&lt;/script&gt;");
    expect(html).toContain("Ваза «Линия»");
    expect(html).toContain("application/json");
    expect(html).toContain("/assets/client.css");
    expect(html).not.toContain("<script>unsafe");
  });
  for (const method of ["GET", "HEAD", "POST"])
    for (const path of ["/", "/api/store", "/missing"])
      for (const status of ["ready", "missing", "error"] as const) {
        it(`${method} ${path} ${status}: статусы, заголовки и тело`, async () => {
          const value: StoreModel =
            status === "ready" ? model() : { status, preview: false, images: {} };
          const res = response();
          await storeHandler(
            async () => value,
            async () => ({ scripts: [], styles: [] }),
          )({ method, url: path } as IncomingMessage, res.value);
          expect(res.value.statusCode).toBe(
            method === "POST"
              ? 405
              : path === "/missing"
                ? 404
                : status === "missing"
                  ? 404
                  : status === "error"
                    ? 503
                    : 200,
          );
          if (method === "HEAD") expect(res.body()).toBe("");
          if (method === "POST") expect(res.headers.get("allow")).toBe("GET, HEAD");
          if (method === "GET" && path !== "/missing") {
            expect(res.headers.get("cache-control")).toBe("no-store");
            expect(res.headers.get("referrer-policy")).toBe("no-referrer");
            expect(res.headers.get("x-content-type-options")).toBe("nosniff");
            if (path === "/api/store") expect(JSON.parse(res.body())).toEqual(value);
            else expect(res.body()).toContain("<!doctype html>");
          }
        });
      }
  it("отклоняет отсутствующий URL", async () => {
    const res = response();
    await storeHandler(
      async () => model(),
      async () => ({ scripts: [], styles: [] }),
    )({ method: "GET" } as IncomingMessage, res.value);
    expect(res.value.statusCode).toBe(404);
  });
  it.each(["GET", "HEAD"])("скрывает исключения инфраструктуры: %s", async (method) => {
    const res = response();
    await storeHandler(
      async () => {
        throw new Error("SECRET");
      },
      async () => ({ scripts: [], styles: [] }),
    )({ method, url: "/" } as IncomingMessage, res.value);
    expect(res.value.statusCode).toBe(503);
    expect(res.body()).toBe(method === "HEAD" ? "" : '{"error":"STORE_UNAVAILABLE"}');
    expect(res.headers.get("cache-control")).toBe("no-store");
  });
  it("применяет преобразование шаблона и игнорирует query", async () => {
    const transform = vi.fn(async (html) => html + "<!-- transformed -->");
    const res = response();
    await storeHandler(
      async () => model(),
      async () => ({ scripts: [], styles: [] }),
      transform,
    )({ method: "GET", url: "/?ignored=1" } as IncomingMessage, res.value);
    expect(res.body()).toContain("transformed");
    expect(transform).toHaveBeenCalledOnce();
  });
});
