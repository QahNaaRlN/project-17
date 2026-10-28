import { afterEach, expect, it, vi } from "vitest";
import { createEditor, type Editor } from "../src/editor.js";
import { StudioError } from "../src/types.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
const editors: Editor[] = [];
afterEach(() => {
  editors.splice(0).forEach((editor) => editor.dispose());
});
async function setup() {
  const { api, session } = mockApi();
  session.history.canUndo = true;
  session.history.undoReason = null;
  const editor = createEditor(api);
  editors.push(editor);
  await editor.open(documentId, changesetId);
  return { api, session, editor };
}
// STU-022: undo/redo run the undo/redo commands.
it("STU-051/053: отмена перечитывает документ, историю и сбрасывает исчезнувшее выделение", async () => {
  const { api, session, editor } = await setup();
  editor.select("n_heading", "one");
  vi.mocked(api.changeHistory).mockImplementation(async () => {
    delete session.record.body.nodes.n_heading;
    session.record.body.nodes.n_root!.children = ["n_custom"];
    session.changeset.seq += 2;
    session.history.seq = session.changeset.seq;
    session.history.canUndo = false;
    session.history.canRedo = true;
  });
  const frame = { postMessage: vi.fn() };
  editor.bindFrame(frame);
  await editor.changeHistory("undo");
  expect(api.changeHistory).toHaveBeenCalledWith(
    expect.objectContaining({ changeset: expect.objectContaining({ seq: 1 }) }),
    "undo",
    expect.any(String),
    expect.any(AbortSignal),
  );
  expect(editor.store.getState()).toMatchObject({
    status: "ready",
    selected: "n_root",
    instance: null,
    session: { history: { seq: 3, canRedo: true } },
  });
  expect(editor.store.getState().document?.nodes.n_heading).toBeUndefined();
  expect(editor.store.getState().historyRevision).toBe(2);
});
it("STU-052: только одна команда; read-only и недоступное действие не отправляются", async () => {
  const { api, session, editor } = await setup();
  let finish!: () => void;
  vi.mocked(api.changeHistory).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const promise = editor.changeHistory("undo");
  await editor.changeHistory("undo");
  await editor.retryHistory();
  await editor.apply([]);
  expect(api.changeHistory).toHaveBeenCalledTimes(1);
  expect(api.apply).not.toHaveBeenCalled();
  finish();
  await promise;
  session.changeset.state = "in_review";
  await editor.refresh();
  await editor.changeHistory("undo");
  expect(api.changeHistory).toHaveBeenCalledTimes(1);
  session.changeset.state = "open";
  session.history.canUndo = false;
  await editor.refresh();
  await editor.changeHistory("undo");
  expect(api.changeHistory).toHaveBeenCalledTimes(1);
});
it("STU-053: потерянный ответ восстанавливает фактическую версию и повторяет только старый ключ/seq", async () => {
  const { api, session, editor } = await setup();
  vi.mocked(api.changeHistory).mockImplementationOnce(async () => {
    session.changeset.seq = 3;
    session.history.seq = 3;
    session.record.body.nodes.n_heading!.props = { level: 1 };
    throw new StudioError("NETWORK_UNCERTAIN", true);
  });
  await editor.changeHistory("undo");
  expect(editor.store.getState()).toMatchObject({
    status: "uncertain",
    historyRetry: true,
    document: { nodes: { n_heading: { props: { level: 1 } } } },
  });
  await editor.apply([]);
  await editor.changeHistory("redo");
  await editor.refresh();
  expect(editor.store.getState().status).toBe("uncertain");
  expect(api.apply).not.toHaveBeenCalled();
  await editor.retryHistory();
  const calls = vi.mocked(api.changeHistory).mock.calls;
  expect(calls).toHaveLength(2);
  expect(calls[0]).toEqual(calls[1]);
  expect(calls[1]![0].changeset.seq).toBe(1);
  expect(editor.store.getState()).toMatchObject({ status: "ready", historyRetry: false });
  await editor.retryHistory();
  expect(api.changeHistory).toHaveBeenCalledTimes(2);
});
it.each(["FORBIDDEN", "CHANGESET_SEQ_CONFLICT", "NOTHING_TO_REDO"])(
  "STU-053: отказ %s перечитывается без автоматического повторения",
  async (code) => {
    const { api, editor } = await setup();
    vi.mocked(api.changeHistory).mockRejectedValueOnce(new StudioError(code));
    await editor.changeHistory("undo");
    expect(editor.store.getState()).toMatchObject({
      status: "ready",
      historyRetry: false,
      error: code,
    });
    await editor.retryHistory();
    expect(api.changeHistory).toHaveBeenCalledTimes(1);
  },
);
it("STU-053: чтение после команды не удалось — следующая запись ждёт явного перечитывания", async () => {
  const { api, editor } = await setup();
  vi.mocked(api.open).mockRejectedValueOnce(Error("secret"));
  await editor.changeHistory("undo");
  expect(editor.store.getState()).toMatchObject({
    status: "uncertain",
    historyRetry: false,
    error: "HISTORY_READ_FAILED",
  });
  await editor.changeHistory("undo");
  expect(api.changeHistory).toHaveBeenCalledTimes(1);
  await editor.refresh();
  expect(editor.store.getState().status).toBe("ready");
});
it("STU-053: неизвестное исключение и закрытие редактора не подменяют состояние другой сессии", async () => {
  const { api, editor } = await setup();
  vi.mocked(api.changeHistory).mockRejectedValueOnce(Error("private credential"));
  await editor.changeHistory("undo");
  expect(editor.store.getState()).toMatchObject({
    status: "uncertain",
    historyRetry: true,
    error: "HISTORY_UNCERTAIN",
  });
  let finish!: (value?: unknown) => void;
  vi.mocked(api.changeHistory).mockImplementation(
    () =>
      new Promise((_, reject) => {
        finish = reject;
      }),
  );
  const pending = editor.retryHistory();
  editor.dispose();
  finish(new StudioError("NETWORK_UNCERTAIN", true));
  await pending;
  expect(editor.store.getState().error).toBeNull();
});
