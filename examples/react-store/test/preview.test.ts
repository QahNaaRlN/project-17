import { createElement, act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { documentHash } from "@cms/runtime";
import * as previewSdk from "@cms/react/preview";
import { StoreApp } from "../src/app.js";
import { loadStore } from "../src/load.js";
import { cms, id, page } from "./fixtures.js";
import { catalog } from "../scripts/catalog.js";
const origin = "https://studio.example",
  manifestHash = "sha256:" + "a".repeat(64);
let root: Root | undefined;
afterEach(async () => {
  await act(async () => root?.unmount());
  root = undefined;
  document.body.replaceChildren();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function model() {
  return loadStore(
    cms(),
    "/store",
    { preview: { token: "private-preview-token", changesetId: id } },
    { studioOrigin: origin, manifestHash },
  );
}
async function mount(initialModel: Awaited<ReturnType<typeof model>>) {
  const container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () => {
    root!.render(createElement(StoreApp, { initialModel }));
  });
}
function post(type: string, payload: unknown, from = origin) {
  const win = document.defaultView!;
  win.dispatchEvent(
    new win.MessageEvent("message", {
      origin: from,
      source: win.parent,
      data: { cms: "preview", v: 1, type, payload, id: "studio" },
    }),
  );
}
it("SDK-030/032: preview snapshot содержит публичный IR/данные, tokens и private config исключены", async () => {
  const c = cms();
  vi.mocked(c.getPage).mockResolvedValue(page({ ...catalog, privateNote: "not-for-browser" }));
  const value = await loadStore(
    c,
    "/store",
    { preview: { token: "private-preview-token" } },
    { studioOrigin: origin, manifestHash },
  );
  expect(value.status).toBe("ready");
  expect(value.plan!.preview).toBe(true);
  expect(value.bridge!.hash).toBe(await documentHash(value.bridge!.document));
  expect(value.bridge!.data.featured).toMatchObject({ items: expect.any(Array) });
  expect(JSON.stringify(value)).not.toMatch(/not-for-browser|private-preview-token/);
  expect(
    (await loadStore(c, "/store", {}, { studioOrigin: origin, manifestHash })).bridge,
  ).toBeUndefined();
});
it("SDK-033/040: витрина выбирает экземпляр в design, исполняет в interact и применяет patch", async () => {
  const initial = await model(),
    send = vi.spyOn(window, "postMessage").mockImplementation(() => {});
  await mount(initial);
  await vi.waitFor(() =>
    expect(send.mock.calls.some(([value]) => value.type === "ready")).toBe(true),
  );
  await act(async () => {
    post("init", { sessionId: "session", changesetId: id, locale: "ru", mode: "design" });
    await vi.waitFor(() =>
      expect(send.mock.calls.filter(([value]) => value.type === "ready")).toHaveLength(2),
    );
  });
  await act(async () => document.querySelector<HTMLButtonElement>(".favorite-button")!.click());
  expect(document.querySelector(".action-notice")).toBeNull();
  expect(send.mock.calls.find(([value]) => value.type === "node.click")![0].payload).toMatchObject({
    nodeId: "n_product",
    instance: "n_products:string%3Avase/n_product",
  });
  await act(async () => {
    post("mode", { mode: "interact" }, "https://evil.example");
  });
  await act(async () => document.querySelector<HTMLButtonElement>(".favorite-button")!.click());
  expect(document.querySelector(".action-notice")).toBeNull();
  await act(async () => {
    post("mode", { mode: "interact" });
  });
  await act(async () => document.querySelector<HTMLButtonElement>(".favorite-button")!.click());
  expect(document.querySelector(".action-notice")!.textContent).toContain("добавлен в избранное");
  const doc = structuredClone(initial.bridge!.document);
  doc.nodes.n_about!.design = { padding: { raw: 12 } };
  const hash = await documentHash(doc),
    rendered = send.mock.calls.filter(([value]) => value.type === "rendered").length;
  await act(async () => {
    post("document.patch", {
      documentId: id,
      baseHash: initial.bridge!.hash,
      hash,
      patch: [{ op: "add", path: "/nodes/n_about/design", value: { padding: { raw: 12 } } }],
    });
    await vi.waitFor(() =>
      expect(send.mock.calls.filter(([value]) => value.type === "rendered")).toHaveLength(
        rendered + 1,
      ),
    );
  });
  expect(document.querySelector(".about")!.className).toMatch(/c-[a-f0-9]+/);
});
it("SDK-040: полный reload использует серверную модель, отказ не заменяет текущий preview", async () => {
  const initial = await model(),
    send = vi.spyOn(window, "postMessage").mockImplementation(() => {});
  await mount(initial);
  await vi.waitFor(() =>
    expect(send.mock.calls.some(([value]) => value.type === "ready")).toBe(true),
  );
  await act(async () => {
    post("init", { sessionId: "session", changesetId: id, locale: "ru", mode: "design" });
  });
  const client = cms();
  vi.mocked(client.getPage).mockResolvedValue(page({ ...catalog, title: "Обновлено сервером" }));
  const next = await loadStore(
    client,
    "/store",
    { preview: { token: "private", changesetId: id } },
    { studioOrigin: origin, manifestHash },
  );
  const fetch = vi
    .fn()
    .mockImplementation(() => Promise.resolve(new Response(JSON.stringify(next))));
  vi.stubGlobal("fetch", fetch);
  await act(async () => {
    post("invalidate", { seq: 1, objectIds: [id] });
    await vi.waitFor(() =>
      expect(send.mock.calls.filter(([value]) => value.type === "rendered")).toHaveLength(2),
    );
  });
  expect(document.querySelector("h1")!.textContent).toContain("Обновлено сервером");
  await act(async () => {
    document.querySelector<HTMLButtonElement>(".refresh-button")!.click();
    await vi.waitFor(() =>
      expect(send.mock.calls.filter(([value]) => value.type === "rendered")).toHaveLength(3),
    );
  });
  fetch.mockResolvedValueOnce(new Response("{}", { status: 503 }));
  await act(async () => {
    post("invalidate", { seq: 2, objectIds: [id] });
    await vi.waitFor(() =>
      expect(send.mock.calls.some(([value]) => value.type === "error")).toBe(true),
    );
  });
  expect(document.querySelector("h1")!.textContent).toContain("Обновлено сервером");
  fetch.mockResolvedValueOnce(new Response("{}"));
  await act(async () => {
    post("invalidate", { seq: 3, objectIds: [id] });
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(4));
  });
});
it("SDK-033: отказ подключения моста показывает безопасное состояние", async () => {
  vi.spyOn(previewSdk, "attachPreviewBridge").mockRejectedValueOnce(new Error("SECRET"));
  await mount(await model());
  expect(document.querySelector(".action-notice")!.textContent).toBe(
    "Предпросмотр недоступен. Обновите страницу.",
  );
  expect(document.body.textContent).not.toContain("SECRET");
});
