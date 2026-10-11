import { expect, it, vi } from "vitest";
import { CmsError, type CmsClient } from "@cms/core";
import {
  createPreviewHandler,
  getPreviewState,
  previewFrameAncestors,
} from "../src/preview-server.js";
import { previewOrigin, previewPath } from "../src/preview-origin.js";
const now = 2_000_000_000_000,
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11";
function token(value: unknown = { exp: now / 1000 + 900, cs: id }) {
  return "header." + Buffer.from(JSON.stringify(value)).toString("base64url") + ".signature";
}
function fixture() {
  const getRoutes = vi.fn().mockResolvedValue({ items: [] });
  const cms = { getRoutes } as unknown as CmsClient;
  const handle = createPreviewHandler({
    cms,
    appOrigin: "https://app.example",
    studioOrigin: "https://studio.example",
    now: () => now,
  });
  const request = (query: string, method = "GET") =>
    new Request("https://app.example/api/cms/preview?" + query, { method });
  return { getRoutes, handle, request };
}
it("SDK-032: cookie устанавливается только после проверки CMS, срок ограничен exp и 900 сек", async () => {
  const { getRoutes, handle, request } = fixture(),
    jwt = token();
  const response = await handle(request("token=" + jwt + "&path=/catalog?locale=ru"));
  expect(response.status).toBe(303);
  expect(response.headers.get("location")).toBe("/catalog?locale=ru");
  expect(getRoutes).toHaveBeenCalledWith({
    preview: { token: jwt },
    signal: expect.any(AbortSignal),
  });
  expect(response.headers.get("set-cookie")).toBe(
    `cms_preview=${jwt}; Path=/; Max-Age=900; HttpOnly; Secure; SameSite=None; Partitioned`,
  );
  expect(response.headers.get("cache-control")).toBe("no-store");
  expect(response.headers.get("referrer-policy")).toBe("no-referrer");
  expect(response.headers.get("content-security-policy")).toBe(
    "frame-ancestors https://studio.example",
  );
  expect(getPreviewState("other=1; cms_preview=" + jwt, now)).toEqual({
    token: jwt,
    changesetId: id,
  });
  expect(
    (await handle(request("token=" + token({ exp: now / 1000 + 30 })))).headers.get("set-cookie"),
  ).toContain("Max-Age=30");
  expect(getPreviewState("cms_preview=" + token({ exp: now / 1000 + 30, cs: null }), now)).toEqual({
    token: token({ exp: now / 1000 + 30, cs: null }),
  });
});
it.each([
  new CmsError("UNAUTHENTICATED", 401),
  new CmsError("FORBIDDEN", 403),
  new Error("PRIVATE TOKEN"),
])("SDK-032: отказ CMS не раскрывает token и не выдаёт cookie: %s", async (cause) => {
  const { getRoutes, handle, request } = fixture();
  getRoutes.mockRejectedValue(cause);
  const response = await handle(request("token=" + token()));
  expect(response.status).toBe(cause instanceof CmsError ? cause.status : 503);
  expect(response.headers.has("set-cookie")).toBe(false);
  expect(await response.text()).toBe('{"error":{"code":"CMS_PREVIEW_VERIFY_FAILED"}}');
});
it("SDK-032: проверка не превращает истёкший за время запроса token в cookie", async () => {
  let time = now;
  const cms = {
    getRoutes: async () => {
      time += 10_000;
      return { items: [] };
    },
  } as unknown as CmsClient;
  const handle = createPreviewHandler({
    cms,
    appOrigin: "https://app.example",
    studioOrigin: "https://studio.example",
    now: () => time,
  });
  expect(
    (
      await handle(
        new Request("https://app.example/api/cms/preview?token=" + token({ exp: now / 1000 + 1 })),
      )
    ).status,
  ).toBe(401);
});
it.each([
  "",
  "token=bad",
  "token=" + token({ exp: now / 1000 }),
  "token=" + token({ exp: "bad" }),
  "token=" + token({ exp: now / 1000 + 1, cs: "bad" }),
  "token=" + token() + "&token=" + token(),
  "token=" + token() + "&path=/safe&path=/other",
  "token=" + token() + "&path=https://evil.example",
  "token=" + token() + "&path=//evil.example",
  "token=" + token() + "&path=/%2fevil.example",
  "token=" + token() + "&path=%2F%5Cevil.example",
])("SDK-032: неверный запрос %s отклоняется до обращения к CMS", async (query) => {
  const { getRoutes, handle, request } = fixture();
  expect((await handle(request(query))).status).toBe(400);
  expect(getRoutes).not.toHaveBeenCalled();
});
it("SDK-032: чужой origin и метод не принимаются, повреждённые/дублирующиеся cookies не читаются", async () => {
  const { handle, request } = fixture();
  expect((await handle(request("token=" + token(), "POST"))).status).toBe(405);
  expect((await handle(new Request("https://evil.example/?token=" + token()))).status).toBe(400);
  for (const cookie of [
    undefined,
    null,
    "cms_preview=bad",
    "cms_preview=%xx",
    "cms_preview=" + token({ exp: now / 1000 }),
    "cms_preview=" + token() + ";cms_preview=" + token(),
    "cms_preview=" + "a".repeat(8193),
  ])
    expect(getPreviewState(cookie, now)).toBeUndefined();
});
it("SDK-031/032: origins и redirect path исключают wildcard, credentials и внедрение CSP", () => {
  expect(previewOrigin("http://127.0.0.1:8080")).toBe("http://127.0.0.1:8080");
  expect(previewFrameAncestors("https://studio.example", false)).toBe("'none'");
  expect(previewFrameAncestors(undefined, true)).toBe("'none'");
  expect(previewFrameAncestors("https://studio.example", true)).toBe("https://studio.example");
  for (const origin of [
    "*",
    "null",
    "https://studio.example/path",
    "https://user:pass@studio.example",
    "http://public.example",
    "https://studio.example/?q=1",
    "https://studio.example/#hash",
  ])
    expect(() => previewOrigin(origin)).toThrow();
  for (const path of [
    "//evil",
    "/\\evil",
    "/has space",
    "/a#hash",
    "/%",
    "/%00",
    "https://evil",
    "/" + "a".repeat(2048),
  ])
    expect(() => previewPath(path)).toThrow();
});
