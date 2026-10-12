import { afterEach, expect, it, vi } from "vitest";
import { createEditor, type Editor } from "../src/editor.js";
import { insertion } from "../src/structure.js";
import { mockApi, documentId, changesetId, persist } from "./fixture.js";
import { StudioError } from "../src/types.js";
let editor: Editor;
afterEach(() => editor?.dispose());
it("STU-064: оптимистичный insert, блокировка записи и authoritative preview без replace missing node", async () => {
  const { api, session } = mockApi();
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  const frame = { postMessage: vi.fn() };
  editor.bindFrame(frame);
  let finish = () => {};
  vi.mocked(api.apply).mockImplementation(async (_s, operations) => {
    await new Promise<void>((resolve) => {
      finish = resolve;
    });
    persist(session, operations);
  });
  const op = insertion(
    documentId,
    { parentId: "n_root", slot: "", index: 0 },
    { id: "n_new_text", type: "Text" },
  );
  const save = editor.apply([op], "n_new_text");
  expect(editor.store.getState().document!.nodes.n_new_text!.type).toBe("Text");
  expect(editor.store.getState().status).toBe("saving");
  await editor.apply([op]);
  expect(api.apply).toHaveBeenCalledTimes(1);
  finish();
  await save;
  expect(editor.store.getState().selected).toBe("n_new_text");
  expect(frame.postMessage.mock.calls.some(([m]) => m.type === "document.patch")).toBe(false);
  expect(frame.postMessage).toHaveBeenLastCalledWith(
    expect.objectContaining({ type: "invalidate" }),
    session.config.appOrigin,
  );
});
it("STU-064: failed/lost удаления перечитывает документ и корректирует выбор", async () => {
  const { api, session } = mockApi();
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select("n_heading");
  vi.mocked(api.apply).mockImplementation(async (_s, ops) => {
    persist(session, ops);
    throw new StudioError("NETWORK_UNCERTAIN", true);
  });
  await editor.apply(
    [{ type: "node.remove", target: documentId, payload: { nodeId: "n_heading" } }],
    "n_root",
  );
  expect(editor.store.getState()).toMatchObject({
    status: "ready",
    selected: "n_root",
    error: "NETWORK_UNCERTAIN",
  });
  expect(editor.store.getState().document!.nodes.n_heading).toBeUndefined();
  await editor.apply([{ type: "node.remove", target: documentId, payload: { nodeId: "n_root" } }]);
  expect(editor.store.getState().error).toBe("ROOT_IMMUTABLE");
});
it("STU-064: отказ вставки сразу восстанавливает существующий выбор до перечитывания", async () => {
  const { api } = mockApi();
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  vi.mocked(api.apply).mockRejectedValueOnce(new StudioError("FORBIDDEN"));
  let finish = () => {};
  vi.mocked(api.open).mockImplementationOnce(async () => {
    await new Promise<void>((resolve) => {
      finish = resolve;
    });
    throw new StudioError("OPEN_FAILED");
  });
  const save = editor.apply(
    [
      insertion(
        documentId,
        { parentId: "n_root", slot: "", index: 0 },
        { id: "n_rejected", type: "Text" },
      ),
    ],
    "n_rejected",
  );
  await vi.waitFor(() => expect(editor.store.getState().status).toBe("uncertain"));
  const state = editor.store.getState();
  expect(state.document!.nodes[state.selected!]).toBeDefined();
  finish();
  await save;
});
