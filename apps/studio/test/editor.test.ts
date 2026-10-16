import { afterEach, expect, it, vi } from "vitest";
import fc from "fast-check";
import { createEditor, type Editor } from "../src/editor.js";
import { StudioError, type Operation } from "../src/types.js";
import { mockApi, documentId, changesetId, persist } from "./fixture.js";
const editors: Editor[] = [];
afterEach(() => {
  editors.forEach((e) => e.dispose());
  editors.length = 0;
  vi.useRealTimers();
});
async function setup() {
  const { api, session } = mockApi(),
    editor = createEditor(api);
  editors.push(editor);
  await editor.open(documentId, changesetId);
  const frame = { postMessage: vi.fn() },
    origin = session.config.appOrigin;
  editor.bindFrame(frame);
  const receive = (
    type: string,
    payload: Record<string, unknown>,
    source: unknown = frame,
    from = origin,
  ) =>
    editor.receive({
      origin: from,
      source,
      data: { cms: "preview", v: 1, type, id: "app-1", payload },
    });
  return { api, session, editor, frame, receive };
}
const design: Operation = {
  type: "node.setDesign",
  target: documentId,
  payload: { nodeId: "n_root", set: { padding: { raw: 12 } } },
};
it("STU-010/026, SDK-032: handshake и точные origin/Window, синхронный выбор Repeat", async () => {
  const { editor, frame, receive } = await setup();
  receive("ready", { documentId, protocolVersion: 1 }, {}, "https://evil.example");
  expect(frame.postMessage).not.toHaveBeenCalled();
  receive("ready", { documentId, protocolVersion: 1 });
  expect(frame.postMessage).toHaveBeenCalledWith(
    expect.objectContaining({
      type: "init",
      payload: expect.objectContaining({ changesetId, mode: "design" }),
    }),
    "http://127.0.0.1:18083",
  );
  receive("ready", { documentId, protocolVersion: 1 });
  expect(editor.store.getState().connected).toBe(true);
  receive("node.click", { nodeId: "n_heading", instance: "repeat/one" });
  expect(editor.store.getState()).toMatchObject({ selected: "n_heading", instance: "repeat/one" });
  editor.select("n_custom");
  expect(editor.store.getState().instance).toBeNull();
  expect(frame.postMessage).toHaveBeenLastCalledWith(
    expect.objectContaining({ type: "select", payload: { nodeId: "n_custom" } }),
    "http://127.0.0.1:18083",
  );
  receive("node.click", { nodeId: "n_foreign" });
  expect(editor.store.getState().selected).toBe("n_custom");
  editor.mode("interact");
  editor.width(375);
  editor.width(99);
  editor.locale("en");
  expect(editor.store.getState()).toMatchObject({ mode: "interact", width: 375, locale: "en" });
  const invalidations = frame.postMessage.mock.calls.filter(
    ([message]) => message.type === "invalidate",
  ).length;
  receive("ready", { documentId, protocolVersion: 1 });
  expect(
    frame.postMessage.mock.calls.filter(([message]) => message.type === "invalidate"),
  ).toHaveLength(invalidations);
});
it("SDK-042: сохраняет только безопасную геометрию своего документа", async () => {
  const { editor, receive } = await setup();
  receive("ready", { documentId, protocolVersion: 1 });
  receive("layout", {
    nodes: {
      n_heading: [
        { x: 1, y: -5, w: 100, h: 20, instance: "one" },
        { x: 0, y: 0, w: -1, h: 20 },
        { x: Infinity, y: 0, w: 1, h: 1 },
        { x: 0, y: 0, w: 1, h: 1, instance: 9 },
      ],
      n_unknown: [{ x: 0, y: 0, w: 1, h: 1 }],
      n_custom: "bad",
    },
  });
  expect(editor.store.getState().rects).toEqual({
    n_heading: [{ x: 1, y: -5, w: 100, h: 20, instance: "one" }],
  });
  receive("error", { code: "CMS_PREVIEW_UPDATE_FAILED" });
  expect(editor.store.getState().previewError).toBe("CMS_PREVIEW_UPDATE_FAILED");
  receive("error", { code: "secret detail" });
  expect(editor.store.getState().previewError).toBe("PREVIEW_FAILED");
  receive("ready", { documentId: "other", protocolVersion: 1 });
  expect(editor.store.getState().connected).toBe(false);
});
it("STU-021: оптимистичная правка и авторитетное сохранение, только одна запись", async () => {
  const { editor, api, session, frame } = await setup();
  let finish: () => void = () => {};
  vi.mocked(api.apply).mockImplementation(async (_s, ops) => {
    await new Promise<void>((resolve) => {
      finish = resolve;
    });
    persist(session, ops);
  });
  const save = editor.apply([design]);
  await vi.waitFor(() => expect(api.apply).toHaveBeenCalledTimes(1));
  expect(editor.store.getState()).toMatchObject({
    status: "saving",
    document: { nodes: { n_root: { design: { padding: { raw: 12 } } } } },
  });
  await editor.apply([design]);
  await editor.open(documentId, changesetId);
  await editor.refresh();
  expect(api.apply).toHaveBeenCalledTimes(1);
  expect(frame.postMessage).toHaveBeenCalledWith(
    expect.objectContaining({ type: "document.patch" }),
    session.config.appOrigin,
  );
  finish();
  await save;
  expect(editor.store.getState()).toMatchObject({
    status: "ready",
    session: { changeset: { seq: 2 } },
  });
  expect(frame.postMessage).toHaveBeenLastCalledWith(
    expect.objectContaining({ type: "invalidate" }),
    session.config.appOrigin,
  );
  const call = vi.mocked(api.apply).mock.calls[0]!;
  expect(call[2]).toMatch(/^[a-f0-9-]{36}$/);
});
it.each(["FORBIDDEN", "CHANGESET_SEQ_CONFLICT"])(
  "STU-021: %s откатывает и перечитывает черновик",
  async (code) => {
    const { editor, api, session } = await setup();
    vi.mocked(api.apply).mockRejectedValue(new StudioError(code));
    session.changeset.seq = 7;
    await editor.apply([design]);
    expect(editor.store.getState()).toMatchObject({
      status: "ready",
      error: code,
      session: { changeset: { seq: 7 } },
    });
    expect(editor.store.getState().document!.nodes.n_root!.design).toBeUndefined();
  },
);
it("неопределённый ответ после commit восстанавливает фактический результат без повторной записи", async () => {
  const { editor, api, session } = await setup();
  vi.mocked(api.apply).mockImplementation(async (_s, ops) => {
    persist(session, ops);
    throw new StudioError("NETWORK_UNCERTAIN", true);
  });
  await editor.apply([design]);
  expect(editor.store.getState().document!.nodes.n_root!.design).toEqual({ padding: { raw: 12 } });
  expect(api.apply).toHaveBeenCalledTimes(1);
});
it("неуспешное восстановление блокирует записи до явного перечитывания", async () => {
  const { editor, api, session } = await setup();
  vi.mocked(api.apply).mockRejectedValue(Error("lost"));
  vi.mocked(api.open).mockRejectedValue(new StudioError("NETWORK_UNCERTAIN"));
  await editor.apply([design]);
  expect(editor.store.getState().status).toBe("uncertain");
  await editor.apply([design]);
  await editor.open(documentId, changesetId);
  expect(api.apply).toHaveBeenCalledTimes(1);
  await editor.refresh();
  expect(editor.store.getState().status).toBe("uncertain");
  vi.mocked(api.open).mockResolvedValue(structuredClone(session));
  await editor.refresh();
  expect(editor.store.getState().status).toBe("ready");
});
it("локальный текст не патчится до commit; read-only CS не изменяется", async () => {
  const { editor, api, session, frame } = await setup();
  await editor.apply([
    {
      type: "localContent.set",
      target: documentId,
      payload: { key: "t_text", type: "text", locale: "ru", value: "Текст" },
    },
    {
      type: "node.setBinding",
      target: documentId,
      payload: { nodeId: "n_heading", prop: "text", binding: "$local.t_text" },
    },
  ]);
  expect(editor.store.getState().document!.localContent).toEqual({
    t_text: { type: "text", value: { ru: "Текст" } },
  });
  expect(frame.postMessage.mock.calls.some(([m]) => m.type === "document.patch")).toBe(false);
  session.changeset.state = "submitted";
  await editor.refresh();
  await editor.apply([design]);
  expect(api.apply).toHaveBeenCalledTimes(1);
});
it("неизвестный узел, устаревшее открытие, dispose и ошибки preview безопасны", async () => {
  const { editor, api, session } = await setup();
  await editor.apply([{ ...design, payload: { nodeId: "n_missing", set: { padding: 1 } } }]);
  expect(editor.store.getState().error).toBe("NODE_NOT_FOUND");
  vi.mocked(api.preview).mockRejectedValue(Error("secret"));
  await editor.retryPreview();
  expect(editor.store.getState().previewError).toBe("PREVIEW_FAILED");
  vi.mocked(api.preview).mockResolvedValue({ token: "expired", expiresAt: "bad" });
  await editor.retryPreview();
  expect(editor.store.getState().previewError).toBe("PREVIEW_INVALID");
  let resolve: (s: typeof session) => void = () => {};
  vi.mocked(api.open).mockImplementationOnce(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const stale = editor.open(documentId, changesetId);
  await editor.open(documentId, changesetId);
  resolve({ ...session, changeset: { ...session.changeset, title: "stale" } });
  await stale;
  expect(editor.store.getState().session!.changeset.title).not.toBe("stale");
  vi.mocked(api.open).mockRejectedValue(Error("secret"));
  await editor.open(documentId, changesetId);
  expect(editor.store.getState()).toMatchObject({ status: "error", error: "OPEN_FAILED" });
  await editor.refresh();
  await editor.retryPreview();
  editor.select("n_root");
  editor.dispose();
});
it("API-041: обновляет preview до истечения и сохраняет локаль/режим", async () => {
  vi.useFakeTimers();
  const { editor, api } = await setup();
  editor.locale("en");
  editor.mode("interact");
  await vi.advanceTimersByTimeAsync(840_000);
  expect(api.preview).toHaveBeenCalledTimes(2);
  expect(editor.store.getState()).toMatchObject({ locale: "en", mode: "interact" });
});
it("SDK-030/032: отсутствие handshake показывает ошибку вместо бесконечного подключения", async () => {
  vi.useFakeTimers();
  const { editor } = await setup();
  await vi.advanceTimersByTimeAsync(10_000);
  expect(editor.store.getState().previewError).toBe("PREVIEW_HANDSHAKE_TIMEOUT");
});
it("SDK-032: произвольные сообщения не бросают исключений и не повреждают IR", async () => {
  const { editor, frame, session } = await setup(),
    before = structuredClone(editor.store.getState().document);
  fc.assert(
    fc.property(fc.jsonValue(), (data) => {
      expect(() =>
        editor.receive({ origin: session.config.appOrigin, source: frame, data }),
      ).not.toThrow();
      expect(editor.store.getState().document).toEqual(before);
    }),
  );
  editor.receive({
    origin: session.config.appOrigin,
    source: frame,
    data: {
      cms: "preview",
      v: 1,
      payload: {
        get bad() {
          throw Error("getter");
        },
      },
    },
  });
  editor.error("FIELD_INVALID");
  expect(editor.store.getState().error).toBe("FIELD_INVALID");
});
