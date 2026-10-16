import { expect, it, vi } from "vitest";
import { documentHash } from "@cms/runtime";
import type { IrDocument } from "@cms/ir";
import {
  createPreviewController,
  type PreviewSnapshot,
  type PreviewControllerOptions,
} from "../src/preview-core.js";
const origin = "https://studio.example",
  manifestHash = "sha256:" + "a".repeat(64);
async function snapshot(label = "one"): Promise<PreviewSnapshot> {
  const document: IrDocument = {
    irVersion: "1.0",
    kind: "page",
    root: "text",
    nodes: { text: { id: "text", type: "Text", props: { text: label } } },
  };
  return { documentId: "document", path: "/page", document, hash: await documentHash(document) };
}
async function fixture(extra: Partial<PreviewControllerOptions> = {}, omitServices = false) {
  const initial = await snapshot(),
    parent = {},
    send = vi.fn(),
    render = vi.fn().mockResolvedValue(undefined),
    reload = vi.fn().mockResolvedValue(await snapshot("server")),
    onMode = vi.fn(),
    onSelect = vi.fn(),
    onHover = vi.fn(),
    onZones = vi.fn(),
    navigate = vi.fn(),
    refreshToken = vi.fn();
  const options = {
    studioOrigin: origin,
    parent,
    initial,
    manifestHash,
    send,
    render,
    reload,
    onMode,
    onSelect,
    onHover,
    onZones,
    navigate,
    refreshToken,
    now: () => 1000,
    ...extra,
  };
  const actual: PreviewControllerOptions = { ...options };
  if (omitServices) {
    delete actual.navigate;
    delete actual.refreshToken;
  }
  const controller = await createPreviewController(actual);
  const message = (type: string, payload: unknown = {}) => ({
    cms: "preview",
    v: 1,
    type,
    id: "test",
    payload,
  });
  const receive = (type: string, payload: unknown = {}) =>
    controller.receive({ origin, source: parent, data: message(type, payload) });
  const init = () => receive("init", { sessionId: "session", locale: "ru", mode: "design" });
  const errors = () =>
    send.mock.calls
      .filter(([message]) => message.type === "error")
      .map(([message]) => message.payload.code);
  return {
    controller,
    initial,
    parent,
    options,
    send,
    render,
    reload,
    onMode,
    onSelect,
    onHover,
    onZones,
    navigate,
    refreshToken,
    message,
    receive,
    init,
    errors,
  };
}
it("SDK-031/041: отправляет только публичные данные точному origin, чужие origin/source и версии не меняют состояние", async () => {
  const f = await fixture();
  expect(f.send.mock.calls[0]![1]).toBe(origin);
  expect(f.send.mock.calls[0]![0].payload).toMatchObject({
    protocolVersion: 1,
    manifestHash,
    documentId: "document",
  });
  expect(f.send.mock.calls[1]![0].payload).toEqual({ hash: f.initial.hash, durationMs: 0 });
  await f.controller.receive({
    origin: "https://evil.example",
    source: f.parent,
    data: f.message("mode", { mode: "interact" }),
  });
  await f.controller.receive({ origin, source: {}, data: f.message("mode", { mode: "interact" }) });
  expect(f.onMode).not.toHaveBeenCalled();
  await f.receive("mode", { mode: "interact" });
  expect(f.errors()).toContain("CMS_PREVIEW_INIT_REQUIRED");
  await f.controller.receive({ origin, source: f.parent, data: { ...f.message("init"), v: 2 } });
  expect(f.errors()).toContain("CMS_PREVIEW_PROTOCOL_UNSUPPORTED");
  await f.init();
  await f.receive("mode", { mode: "interact" });
  expect(f.controller.mode).toBe("interact");
  expect(f.onMode).toHaveBeenLastCalledWith("interact");
});
it("SDK-033/042: init сверяет Change Set, callbacks selection/hover/zones/navigation не создают редактор", async () => {
  const f = await fixture({ changesetId: "cs" });
  await f.init();
  expect(f.errors()).toContain("CMS_PREVIEW_INIT_INVALID");
  await f.receive("init", {
    sessionId: "session",
    changesetId: "cs",
    locale: "en",
    mode: "interact",
  });
  expect(f.reload).toHaveBeenCalledWith(expect.any(AbortSignal), "en");
  for (const id of ["text", null]) {
    await f.receive("select", { nodeId: id });
    await f.receive("hover", { nodeId: id });
  }
  expect(f.onSelect).toHaveBeenLastCalledWith(null);
  expect(f.onHover).toHaveBeenLastCalledWith(null);
  await f.receive("zones.show", { enabled: true });
  expect(f.onZones).toHaveBeenCalledWith(true);
  await f.receive("navigate", { path: "/next" });
  expect(f.navigate).toHaveBeenCalledWith("/next");
  await f.receive("token.refresh");
  expect(f.refreshToken).toHaveBeenCalledWith(expect.any(AbortSignal));
  expect(f.reload).toHaveBeenCalledTimes(2);
  expect(JSON.stringify(f.send.mock.calls)).not.toContain("token:");
});
it("SDK-040: patch с совпавшим hash применяется без reload, плохой baseHash/hash/IR перечитывает черновик", async () => {
  const f = await fixture();
  await f.init();
  const next = await snapshot("edited");
  await f.receive("document.patch", {
    documentId: "document",
    baseHash: f.initial.hash,
    hash: next.hash,
    patch: [{ op: "replace", path: "/nodes/text/props/text", value: "edited" }],
  });
  expect(f.controller.snapshot.document.nodes.text!.props).toEqual({ text: "edited" });
  expect(f.reload).not.toHaveBeenCalled();
  expect(f.render).toHaveBeenCalledWith(next);
  for (const payload of [
    { baseHash: manifestHash, hash: next.hash, patch: [] },
    { baseHash: next.hash, hash: manifestHash, patch: [] },
    {
      baseHash: (await snapshot("server")).hash,
      hash: next.hash,
      patch: [{ op: "remove", path: "/nodes/text" }],
    },
  ])
    await f.receive("document.patch", { documentId: "document", ...payload });
  expect(f.reload).toHaveBeenCalledTimes(3);
  expect(f.controller.snapshot.document.nodes.text!.props).toEqual({ text: "server" });
  const clone = f.controller.snapshot;
  clone.document.nodes.text!.props = { text: "mutated" };
  expect(f.controller.snapshot.document.nodes.text!.props).toEqual({ text: "server" });
});
it("SDK-040: ошибка reload/renderer сохраняет snapshot и возвращает безопасный код", async () => {
  const f = await fixture();
  await f.init();
  f.reload.mockRejectedValueOnce(new Error("SECRET"));
  await f.receive("invalidate", { seq: 1, objectIds: ["document"] });
  expect(f.controller.snapshot).toEqual(f.initial);
  expect(f.errors()).toContain("CMS_PREVIEW_UPDATE_FAILED");
  // The same event is retryable after the failed reload.
  await f.receive("invalidate", { seq: 1, objectIds: ["document"] });
  expect(f.reload).toHaveBeenCalledTimes(2);
  f.render.mockRejectedValueOnce(new Error("SECRET"));
  const before = f.controller.snapshot;
  await f.receive("invalidate", { seq: 2, objectIds: [] });
  expect(f.controller.snapshot).toEqual(before);
  expect(JSON.stringify(f.send.mock.calls)).not.toContain("SECRET");
});
it("SDK-014/040: изменение источников данных перечитывает серверный результат вместо исполнения в браузере", async () => {
  const f = await fixture();
  await f.init();
  const document = { ...f.initial.document, dataSources: { items: { source: "test.items" } } };
  await f.receive("document.patch", {
    documentId: "document",
    baseHash: f.initial.hash,
    hash: await documentHash(document),
    patch: [{ op: "add", path: "/dataSources", value: document.dataSources }],
  });
  expect(f.reload).toHaveBeenCalledOnce();
  expect(f.controller.snapshot.document.nodes.text!.props).toEqual({ text: "server" });
});
it("SDK-040: invalidate seq упорядочен, dispose отменяет запрос и запрещает поздний render", async () => {
  const f = await fixture();
  await f.init();
  await f.receive("invalidate", { seq: 0, objectIds: [] });
  await f.receive("invalidate", { seq: 0, objectIds: [] });
  expect(f.reload).toHaveBeenCalledTimes(1);
  let finish!: (value: PreviewSnapshot) => void;
  f.reload.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const pending = f.receive("invalidate", { seq: 1, objectIds: [] });
  await vi.waitFor(() => expect(f.reload).toHaveBeenCalledTimes(2));
  const signal = f.reload.mock.calls[1]![0] as AbortSignal;
  f.controller.dispose();
  expect(signal.aborted).toBe(true);
  finish(await snapshot("late"));
  await pending;
  expect(f.render).toHaveBeenCalledTimes(1);
  const count = f.send.mock.calls.length;
  f.controller.notify("error", {});
  await f.receive("mode", { mode: "interact" });
  expect(f.send).toHaveBeenCalledTimes(count);
});
it.each([
  ["init", { sessionId: "", locale: "ru", mode: "design" }],
  ["mode", { mode: "bad" }],
  ["select", { nodeId: "missing" }],
  ["hover", { nodeId: 2 }],
  ["zones.show", { enabled: "true" }],
  ["navigate", { path: "https://evil.example" }],
  ["invalidate", { seq: -1, objectIds: [] }],
  ["invalidate", { seq: 1, objectIds: [2] }],
  [
    "document.patch",
    { documentId: "wrong", baseHash: manifestHash, hash: manifestHash, patch: [] },
  ],
  ["unknown", {}],
])("SDK-031/040: некорректный payload %s не исполняется", async (type, payload) => {
  const f = await fixture();
  await f.init();
  await f.receive(type as string, payload);
  expect(f.errors().length).toBeGreaterThan(0);
  expect(f.controller.snapshot).toEqual(f.initial);
});
it("SDK-031: не-JSON/oversized сообщения, отсутствующие adapters и некорректная начальная модель отклоняются", async () => {
  const f = await fixture({}, true);
  await f.init();
  const cyclic: Record<string, unknown> = {};
  cyclic.self = cyclic;
  const accessor = Object.defineProperty({}, "x", {
    get: () => {
      throw new Error("getter");
    },
    enumerable: true,
  });
  for (const data of [
    null,
    [],
    new Date(),
    cyclic,
    accessor,
    { x: undefined },
    { [Symbol("x")]: 1 },
    { x: "a".repeat(256 * 1024) },
    { ...f.message("mode"), id: "" },
    { ...f.message("mode"), payload: null },
    { ...f.message("mode"), cms: "other" },
  ])
    await f.controller.receive({ origin, source: f.parent, data });
  expect(f.errors()).toContain("CMS_PREVIEW_MESSAGE_INVALID");
  await f.receive("navigate", { path: "/safe" });
  await f.receive("token.refresh");
  expect(f.errors()).toContain("CMS_PREVIEW_NAVIGATE_UNAVAILABLE");
  expect(f.errors()).toContain("CMS_PREVIEW_REFRESH_UNAVAILABLE");
  await expect(createPreviewController({ ...f.options, manifestHash: "bad" })).rejects.toThrow(
    "CMS_PREVIEW_MANIFEST_INVALID",
  );
  await expect(
    createPreviewController({ ...f.options, initial: { ...f.initial, hash: manifestHash } }),
  ).rejects.toThrow("CMS_PREVIEW_DOCUMENT_INVALID");
});
it("SDK-031: очередь ограничена, после dispose отложенные сообщения не исполняются", async () => {
  const f = await fixture();
  await f.init();
  const pending = Array.from({ length: 40 }, () => f.receive("mode", { mode: "interact" }));
  f.controller.dispose();
  await Promise.all(pending);
  expect(f.onMode).toHaveBeenCalledTimes(1);
});
it("SDK-040: refresh хоста обновляет snapshot в той же очереди и сохраняет его при отказе", async () => {
  const f = await fixture();
  await f.init();
  await f.controller.refresh();
  expect(f.controller.snapshot.document.nodes.text!.props).toEqual({ text: "server" });
  f.reload.mockRejectedValueOnce(new Error("private"));
  await f.controller.refresh();
  expect(f.controller.snapshot.document.nodes.text!.props).toEqual({ text: "server" });
  expect(f.errors()).toContain("CMS_PREVIEW_UPDATE_FAILED");
  f.controller.dispose();
  await f.controller.refresh();
  expect(f.reload).toHaveBeenCalledTimes(2);
});
it("STU-231: changing locale aborts the old request and ignores its late result even if the loader ignores abort", async () => {
  const f = await fixture();
  await f.init();
  let finish!: (value: PreviewSnapshot) => void, oldSignal!: AbortSignal;
  f.reload.mockImplementationOnce((signal: AbortSignal) => {
    oldSignal = signal;
    return new Promise<PreviewSnapshot>((resolve) => {
      finish = resolve;
    });
  });
  const first = f.receive("invalidate", { seq: 1, objectIds: ["document"] });
  await vi.waitFor(() => expect(f.reload).toHaveBeenCalledTimes(1));
  f.reload.mockResolvedValueOnce(await snapshot("English"));
  const latest = f.receive("init", { sessionId: "session", locale: "en", mode: "design" });
  expect(oldSignal.aborted).toBe(true);
  finish(await snapshot("stale Russian"));
  await Promise.all([first, latest]);
  expect(f.render).toHaveBeenCalledTimes(1);
  expect(f.controller.snapshot.document.nodes.text!.props).toEqual({ text: "English" });
  expect(f.errors()).toEqual([]);
});
it("STU-231: superseded locale messages cannot reload or commit an intermediate context", async () => {
  const f = await fixture();
  await f.init();
  let finish!: (value: PreviewSnapshot) => void;
  f.reload.mockImplementationOnce(
    () =>
      new Promise<PreviewSnapshot>((resolve) => {
        finish = resolve;
      }),
  );
  const first = f.controller.refresh();
  await vi.waitFor(() => expect(f.reload).toHaveBeenCalledTimes(1));
  const en = f.receive("init", { sessionId: "session", locale: "en", mode: "design" });
  const ru = f.receive("init", { sessionId: "session", locale: "ru", mode: "design" });
  f.reload.mockResolvedValueOnce(await snapshot("latest Russian"));
  finish(await snapshot("stale"));
  await Promise.all([first, en, ru]);
  expect(f.reload).toHaveBeenCalledTimes(2);
  expect(f.reload.mock.calls[1]![1]).toBe("ru");
  expect(f.render).toHaveBeenCalledTimes(1);
});
it("STU-231/230: a foreign or invalid init cannot cancel an authorized source load", async () => {
  const f = await fixture();
  await f.init();
  let finish!: (value: PreviewSnapshot) => void, signal!: AbortSignal;
  f.reload.mockImplementationOnce((value: AbortSignal) => {
    signal = value;
    return new Promise<PreviewSnapshot>((resolve) => {
      finish = resolve;
    });
  });
  const first = f.controller.refresh();
  await vi.waitFor(() => expect(f.reload).toHaveBeenCalledTimes(1));
  await f.controller.receive({
    origin: "https://other.example",
    source: f.parent,
    data: f.message("init", { sessionId: "session", locale: "en", mode: "design" }),
  });
  const invalid = f.receive("init", {
    sessionId: "session",
    changesetId: "other",
    locale: "en",
    mode: "design",
  });
  expect(signal.aborted).toBe(false);
  finish(await snapshot("valid"));
  await Promise.all([first, invalid]);
  expect(f.controller.snapshot.document.nodes.text!.props).toEqual({ text: "valid" });
  expect(f.errors()).toContain("CMS_PREVIEW_INIT_INVALID");
});
