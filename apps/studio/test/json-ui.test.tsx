// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { EditorView } from "@codemirror/view";
import { startCompletion, currentCompletions } from "@codemirror/autocomplete";
import { Studio } from "../src/ui.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, persist, documentId, changesetId } from "./fixture.js";
import { StudioError } from "../src/types.js";
import { webcrypto } from "node:crypto";
let root: Root, editor: Editor, client: QueryClient;
const rangeMethods = ["getClientRects", "getBoundingClientRect"] as const;
const originalRangeMethods = rangeMethods.map((name) =>
  Object.getOwnPropertyDescriptor(Range.prototype, name),
);
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  rangeMethods.forEach((name, i) => {
    const original = originalRangeMethods[i];
    if (original) Object.defineProperty(Range.prototype, name, original);
    else Reflect.deleteProperty(Range.prototype, name);
  });
});
const button = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === name)!;
async function click(name: string) {
  await act(async () => button(name).click());
  await vi.waitFor(async () => {
    await act(async () => {
      await Promise.resolve();
    });
    expect(editor.store.getState().status).not.toBe("saving");
  });
}
const view = () => EditorView.findFromDOM(document.querySelector<HTMLElement>(".cm-editor")!)!;
async function input(value: string) {
  await act(async () =>
    view().dispatch({ changes: { from: 0, to: view().state.doc.length, insert: value } }),
  );
}
async function setup() {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  window.history.replaceState({}, "", "/editor");
  vi.stubGlobal("crypto", webcrypto);
  Object.defineProperty(Range.prototype, "getClientRects", { configurable: true, value: () => [] });
  Object.defineProperty(Range.prototype, "getBoundingClientRect", {
    configurable: true,
    value: () => new DOMRect(),
  });
  const { api, session } = mockApi();
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select("n_heading");
  client = new QueryClient();
  document.body.innerHTML = '<div id="root"></div>';
  root = createRoot(document.getElementById("root")!);
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <Studio api={api} editor={editor} />
      </QueryClientProvider>,
    ),
  );
  await click("Structure");
  await vi.waitFor(async () => {
    await act(async () => {
      await Promise.resolve();
    });
    expect(document.querySelector(".cm-editor")).not.toBeNull();
  });
  return { api, session };
}
it("STU-090/091/093/094: JSON узла показывает diff, сохраняется одним действием и защищает выбор", async () => {
  const { api, session } = await setup();
  expect(view().state.doc.toString()).toContain("n_heading");
  const value = JSON.parse(view().state.doc.toString());
  value.name = "JSON title";
  value.props.level = 3;
  await input(JSON.stringify(value, null, 2));
  expect(document.body.textContent).toContain("node.rename");
  expect(api.apply).not.toHaveBeenCalled();
  expect(button("Визуальный").disabled).toBe(true);
  await act(async () => editor.select("n_custom"));
  expect(editor.store.getState().selected).toBe("n_heading");
  await click("Сохранить JSON");
  expect(api.apply).toHaveBeenCalledTimes(1);
  expect(session.record.body.nodes.n_heading).toMatchObject({
    name: "JSON title",
    props: { level: 3 },
  });
  expect(editor.store.getState().dirtyFields.json).toBe(false);
  expect(button("Визуальный").disabled).toBe(false);
  await act(async () => editor.select("n_custom"));
  expect(view().state.doc.toString()).toContain("n_custom");
  await click("Визуальный");
  expect(document.querySelector(".cm-editor")).toBeNull();
});
it("STU-243/244/251: Structure retains on/when input when its separate edit permission is missing", async () => {
  const { api } = await setup();
  await act(async () => {
    const session = editor.store.getState().session!;
    editor.store.setState({
      session: { ...session, review: { ...session.review, canEditCondition: false } },
    });
  });
  const value = JSON.parse(view().state.doc.toString());
  value.when = { op: "exists", arg: "$context.locale" };
  await input(JSON.stringify(value, null, 2));
  await click("Сохранить JSON");
  expect(api.apply).not.toHaveBeenCalled();
  expect(editor.store.getState().error).toBe("FORBIDDEN");
  expect(editor.store.getState().dirtyFields.json).toBe(true);
  expect(JSON.parse(view().state.doc.toString()).when).toEqual(value.when);
  expect(editor.store.getState().document!.nodes.n_heading!.when).toBeUndefined();
});
it("STU-090/092: синтаксис и ID диагностируются, переход к проблеме выделяет JSON", async () => {
  await setup();
  await input("{");
  expect(document.body.textContent).toContain("JSON_SYNTAX");
  expect(button("Сохранить JSON").disabled).toBe(true);
  await click("Сбросить ввод");
  const n = JSON.parse(view().state.doc.toString());
  n.id = "n_other";
  await input(JSON.stringify(n));
  const issue = [...document.querySelectorAll<HTMLButtonElement>(".json-diagnostics button")][0]!;
  await act(async () => issue.click());
  expect(view().state.selection.main.from).toBeGreaterThan(0);
  expect(document.activeElement?.getAttribute("aria-label")).toBe("Редактор JSON");
  await click("Сбросить ввод");
  await input(JSON.stringify(n));
  await click("Сбросить ввод");
  await act(async () => editor.locale("en"));
  expect(button("Save JSON").disabled).toBe(true);
  expect(document.querySelector('[aria-label="JSON editor"]')).not.toBeNull();
});
it("STU-090: whole-page cursor selects the same node; schema/manifest suggestions and input undo stay local", async () => {
  const { api } = await setup();
  await act(async () => {
    const select = document.querySelector<HTMLSelectElement>('[aria-label="Область JSON"]')!;
    select.value = "document";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const at = view().state.doc.toString().indexOf('"n_custom":');
  await act(async () => view().dispatch({ selection: { anchor: at + 16 } }));
  expect(editor.store.getState().selected).toBe("n_custom");
  await act(async () => {
    const select = document.querySelector<HTMLSelectElement>('[aria-label="Область JSON"]')!;
    select.value = "node";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const original = view().state.doc.toString(),
    typed = original.replace('"title"', '"ti"');
  await input(typed);
  const pos = typed.indexOf('"ti"') + 3;
  await act(async () => {
    view().dispatch({ selection: { anchor: pos } });
    startCompletion(view());
  });
  await act(async () => {
    await vi.waitFor(() =>
      expect(currentCompletions(view().state).some((c) => c.label === "title")).toBe(true),
    );
  });
  await click("Сбросить ввод");
  const before = view().state.doc.toString();
  await act(async () =>
    view().dispatch({ changes: { from: before.length - 1, insert: " " }, userEvent: "input.type" }),
  );
  await act(async () => {
    view().focus();
    view().contentDOM.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "z",
        code: "KeyZ",
        ctrlKey: true,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  expect(api.changeHistory).not.toHaveBeenCalled();
  await click("Форматировать JSON");
  expect(document.body.textContent).toContain("Нет изменений документа");
});
it("STU-093: concurrent seq keeps typed JSON and requires explicit discard", async () => {
  const { session } = await setup();
  const n = JSON.parse(view().state.doc.toString());
  n.name = "unsaved";
  await input(JSON.stringify(n));
  session.changeset.seq++;
  session.history.seq++;
  session.record.body.nodes.n_heading!.name = "server latest";
  await act(async () => editor.refresh());
  expect(view().state.doc.toString()).toContain("unsaved");
  expect(document.body.textContent).toContain("Черновик изменился после начала ввода");
  expect(button("Сохранить JSON").disabled).toBe(true);
  await click("Сбросить ввод");
  expect(view().state.doc.toString()).toContain("server latest");
});
it.each([false, true])(
  "STU-093: rejection/lost response rereads server without replay (committed=%s)",
  async (committed) => {
    const { api, session } = await setup();
    const n = JSON.parse(view().state.doc.toString());
    n.name = "attempt";
    await input(JSON.stringify(n));
    expect(
      button("Сохранить JSON").disabled,
      document.body.textContent + JSON.stringify(editor.store.getState().dirtyFields),
    ).toBe(false);
    vi.mocked(api.apply).mockImplementation(async (_session, ops) => {
      if (committed) persist(session, ops);
      throw new StudioError(committed ? "NETWORK_UNCERTAIN" : "FORBIDDEN", committed);
    });
    await click("Сохранить JSON");
    expect(api.apply).toHaveBeenCalledTimes(1);
    expect(view().state.doc.toString()).toContain("attempt");
    expect(editor.store.getState().document?.nodes.n_heading?.name).toBe(
      committed ? "attempt" : undefined,
    );
    await click("Сбросить ввод");
    expect(view().state.doc.toString().includes("attempt")).toBe(committed);
  },
);
it("STU-093: closed or foreign work is read-only; keyboard save does not send", async () => {
  const { api } = await setup();
  await act(async () => {
    const s = structuredClone(editor.store.getState().session!);
    s.review.canEdit = false;
    s.changeset.state = "approved";
    editor.store.setState({ session: s });
  });
  expect(view().contentDOM.getAttribute("contenteditable")).toBe("false");
  await act(async () =>
    view().contentDOM.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        code: "Enter",
        ctrlKey: true,
        bubbles: true,
        cancelable: true,
      }),
    ),
  );
  expect(api.apply).not.toHaveBeenCalled();
  expect(document.body.textContent).toContain("JSON доступен только для чтения");
});
