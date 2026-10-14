// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Studio } from "../src/ui.js";
import { DeferredPanel } from "../src/deferred-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import { StudioError } from "../src/types.js";
import type { HistoryAction } from "../src/types.js";
let root: Root, editor: Editor, client: QueryClient;
afterEach(async () => {
  await act(async () => {
    root?.unmount();
  });
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
async function setup(deepLink = false) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  window.history.replaceState(
    {},
    "",
    deepLink ? `/#document=${documentId}&changeset=${changesetId}` : "/",
  );
  const { api, session } = mockApi();
  editor = createEditor(api);
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  document.body.innerHTML = '<div id="test-root"></div>';
  root = createRoot(document.getElementById("test-root")!);
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <Studio {...{ api, editor }} />
      </QueryClientProvider>,
    );
  });
  await flush();
  return { api, session };
}
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}
async function change(element: HTMLInputElement | HTMLSelectElement, value: string) {
  await act(async () => {
    const prototype =
      element.tagName === "SELECT"
        ? window.HTMLSelectElement.prototype
        : window.HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value")!.set!.call(element, value);
    element.dispatchEvent(
      new Event(element.tagName === "SELECT" ? "change" : "input", { bubbles: true }),
    );
  });
}
async function click(element: HTMLElement) {
  await act(async () => {
    element.click();
  });
  await flush();
}
async function submit(element: HTMLFormElement) {
  await act(async () => {
    element.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  await flush();
  // A preview hash and the command response may outlive one event-loop tick.
  await vi.waitFor(async () => {
    await act(async () => {
      await Promise.resolve();
    });
    expect(editor.store.getState().status).not.toBe("saving");
  });
}
const input = (id: string) => document.getElementById(id) as HTMLInputElement;
const layer = (id: string) =>
  document.querySelector<HTMLElement>(`[role=treeitem][title="${id}"]`)!;
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (item) => item.textContent === text,
  )!;
it("STU-137/139: фактический документ и CS видны; сброс ввода явный и не записывает операции", async () => {
  const { api, session } = await setup(true);
  await vi.waitFor(() => expect(layer("n_heading")).not.toBeNull());
  expect(document.querySelector(".editor-document")!.textContent).toContain(session.record.path);
  expect(document.querySelector(".editor-document")!.textContent).toContain(
    session.changeset.title,
  );
  expect(document.querySelector<HTMLDetailsElement>(".editor-selector")!.open).toBe(false);
  await click(layer("n_heading"));
  await change(input("property-text"), "Unsaved private text");
  expect(document.querySelector(".editor-save-state")!.textContent).toContain(
    "Есть несохранённый ввод",
  );
  expect(button("Перечитать черновик").disabled).toBe(true);
  expect(button("Страницы").disabled).toBe(true);
  await click(layer("n_root"));
  expect(editor.store.getState().selected).toBe("n_heading");
  expect(input("property-text").value).toBe("Unsaved private text");
  expect(button("Сохранить level").disabled).toBe(true);
  const reads = vi.mocked(api.open).mock.calls.length;
  await act(async () => {
    await editor.open(documentId, changesetId);
    editor.viewMode("structure");
  });
  expect(api.open).toHaveBeenCalledTimes(reads);
  expect(editor.store.getState().viewMode).toBe("visual");
  await click(button("Сбросить ввод и перечитать"));
  expect(input("property-text").value).not.toBe("Unsaved private text");
  expect(Object.values(editor.store.getState().dirtyFields).some(Boolean)).toBe(false);
  expect(api.apply).not.toHaveBeenCalled();
  expect(button("Страницы").disabled).toBe(false);
});
it("STU-138/141: вписывание меняет масштаб, сохраняя viewport приложения и координаты overlay", async () => {
  let measure: (() => void) | undefined;
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private callback: () => void) {}
      observe(element: HTMLElement) {
        if (element.classList.contains("canvas-scroll")) measure = this.callback;
      }
      disconnect() {}
    },
  );
  await setup(true);
  const canvas = document.querySelector<HTMLElement>(".canvas-scroll")!;
  Object.defineProperty(canvas, "clientWidth", { value: 560 });
  canvas.style.paddingLeft = "24px";
  canvas.style.paddingRight = "24px";
  await act(async () => {
    measure!();
  });
  const frame = document.querySelector<HTMLElement>(".frame-wrap")!;
  expect(frame.style.width).toBe("1024px");
  expect(frame.style.transform).toBe("scale(0.5)");
  expect(document.querySelector<HTMLElement>(".canvas-zoom")!.style.width).toBe("512px");
  await click(button("Вписать · 50%"));
  expect(frame.style.transform).toBe("scale(1)");
  expect(editor.store.getState().width).toBe(1024);
});
it("STU-140: отказ панели безопасен; перезагрузка недоступна при несохранённом вводе", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
  function Broken(): never {
    throw new Error("private implementation detail");
  }
  document.body.innerHTML = '<div id="test-root"></div>';
  root = createRoot(document.getElementById("test-root")!);
  await act(async () => {
    root.render(
      <DeferredPanel locale="ru" canReload={false}>
        <Broken />
      </DeferredPanel>,
    );
  });
  expect(document.querySelector('[role="alert"]')!.textContent).toContain(
    "Не удалось загрузить панель",
  );
  expect(document.body.textContent).not.toContain("private implementation");
  expect(button("Перезагрузить Studio").disabled).toBe(true);
  await act(async () => {
    root.render(
      <DeferredPanel locale="en" canReload={true}>
        <Broken />
      </DeferredPanel>,
    );
  });
  expect(document.body.textContent).toContain("Could not load the editor panel");
  expect(button("Reload Studio").disabled).toBe(false);
  consoleError.mockRestore();
});
it("STU-131/133: каталог выбирает страницу явно, не выдаёт head за публикацию и сохраняет сессию при возврате", async () => {
  const { api } = await setup();
  expect(document.querySelector(".pages-table")?.textContent).toContain("Store");
  expect(document.querySelector(".pages-table")?.textContent).toContain("В каталоге");
  expect(document.querySelector(".pages-table")?.textContent).not.toContain("Опубликовано");
  await click(document.querySelector<HTMLElement>('[aria-label="Выбрать страницу /store"]')!);
  expect(api.open).not.toHaveBeenCalled();
  await change(
    document.querySelector<HTMLSelectElement>('[aria-label="Мой черновик"]')!,
    changesetId,
  );
  await submit(document.querySelector<HTMLFormElement>(".open-bar")!);
  expect(document.querySelector<HTMLElement>(".editor-surface")!.hidden).toBe(false);
  const current = editor.store.getState().session;
  await click(button("Страницы"));
  expect(document.querySelector(".pages-table")).not.toBeNull();
  expect(editor.store.getState().session).toBe(current);
  await act(async () => {
    editor.formDirty("property-title", true);
  });
  expect(
    [...document.querySelectorAll<HTMLButtonElement>(".studio-navigation button")].every(
      (b) => b.disabled,
    ),
  ).toBe(true);
  expect(document.querySelector<HTMLSelectElement>('[aria-label="Страница"]')!.disabled).toBe(true);
  expect(
    document.querySelector<HTMLButtonElement>('[aria-label="Выбрать страницу /store"]')!.disabled,
  ).toBe(true);
});
it("STU-132/134: поиск передаётся API, различает пустой результат и сохраняет строки при ошибке пагинации", async () => {
  const { api } = await setup();
  vi.mocked(api.pages).mockImplementation(async (cursor, _signal, q) => {
    if (cursor) throw Error("secret");
    return {
      items:
        q === "missing" ? [] : [{ id: documentId, path: "/store", title: "Store", hasHead: true }],
      nextCursor: q === "Store" ? documentId : null,
    };
  });
  await change(
    document.querySelector<HTMLInputElement>('[aria-label="Поиск страниц"]')!,
    " Store ",
  );
  await submit(document.querySelector<HTMLFormElement>(".page-search")!);
  expect(api.pages).toHaveBeenLastCalledWith(undefined, expect.any(AbortSignal), "Store");
  await click(button("Ещё страницы"));
  expect(document.querySelector(".pages-table")?.textContent).toContain("Store");
  expect(document.body.textContent).toContain("Уже загруженные доступны");
  expect(document.body.textContent).not.toContain("secret");
  await change(
    document.querySelector<HTMLInputElement>('[aria-label="Поиск страниц"]')!,
    "missing",
  );
  await submit(document.querySelector<HTMLFormElement>(".page-search")!);
  expect(document.body.textContent).toContain("Нет страниц, соответствующих поиску");
  await click(button("Сбросить поиск"));
  expect(document.querySelector(".pages-table")?.textContent).toContain("Store");
  expect(api.open).not.toHaveBeenCalled();
});
it("STU-130/135: диалог нового черновика удерживает фокус, Escape отменяет только подтверждённо безопасное состояние", async () => {
  const { api } = await setup();
  await selectPage();
  await click(button("Новый черновик"));
  expect(document.activeElement).toBe(document.querySelector('[aria-label="Название черновика"]'));
  await act(async () => {
    document.activeElement!.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Tab", shiftKey: true, bubbles: true, cancelable: true }),
    );
  });
  expect(document.activeElement).toBe(button("Отмена"));
  await act(async () => {
    document.activeElement!.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }),
    );
  });
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(api.createDraft).not.toHaveBeenCalled();
});
function action(
  kind: HistoryAction["kind"],
  type: string,
  id = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c01",
): HistoryAction {
  return {
    id,
    kind,
    firstSeq: 1,
    lastSeq: 1,
    actorId: changesetId,
    actorName: "Designer",
    createdAt: "2026-10-12T00:00:00.000Z",
    reason: null,
    operations: [{ type, target: documentId, status: "applied" }],
  };
}
it("STU-050: история показывает группы/автора, пагинацию и отказ чтения без сырых значений", async () => {
  const { api, session } = await setup(true);
  session.history.items = [action("edit", "localContent.set")];
  session.history.items[0]!.firstSeq = 1;
  session.history.items[0]!.lastSeq = 2;
  session.changeset.seq = 2;
  session.history.seq = 2;
  session.history.nextBeforeSeq = 2;
  await act(async () => {
    await editor.refresh();
  });
  await flush();
  expect(document.body.textContent).toContain("Изменение текста");
  expect(document.body.textContent).toContain("Designer");
  expect(document.body.textContent).toContain("#1–2");
  vi.mocked(api.history).mockRejectedValueOnce(Error("secret"));
  await click(button("Более ранние действия"));
  await flush();
  expect(document.body.textContent).toContain("Не удалось загрузить историю");
  expect(document.body.textContent).not.toContain("secret");
  await click(button("Перечитать черновик"));
  await flush();
  const older = action("undo", "node.setBinding", "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c02");
  vi.mocked(api.history).mockResolvedValue({
    ...session.history,
    items: [older],
    nextBeforeSeq: null,
  });
  await click(button("Более ранние действия"));
  await flush();
  expect(api.history).toHaveBeenLastCalledWith(expect.any(Object), 2, expect.any(AbortSignal));
  expect(document.querySelectorAll(".history-panel li")).toHaveLength(2);
});
it("STU-060/062/064: палитра, обязательные свойства, move/duplicate/delete и английский UI", async () => {
  const { session, api } = await setup(true);
  expect(document.body.textContent).toContain("Для перемещения выберите место вставки");
  const component = () =>
    [...document.querySelectorAll<HTMLSelectElement>("select")].find((s) =>
      s.closest("label")?.textContent?.startsWith("Компонент"),
    )!;
  const add = () => button("Добавить блок");
  await change(component(), "Stack");
  await click(add());
  const stack = editor.store.getState().selected!;
  expect(session.record.body.nodes[stack]!.type).toBe("Stack");
  await change(component(), "Heading");
  await click(add());
  const heading = editor.store.getState().selected!;
  expect(session.record.body.nodes[stack]!.children).toEqual([heading]);
  expect(api.apply).toHaveBeenLastCalledWith(
    expect.anything(),
    [
      expect.objectContaining({ type: "localContent.set" }),
      expect.objectContaining({ type: "node.insert" }),
    ],
    expect.any(String),
    expect.any(AbortSignal),
  );
  await click(button("Дублировать"));
  expect(session.record.body.nodes[stack]!.children).toHaveLength(2);
  await click(button("Удалить"));
  expect(editor.store.getState().selected).toBe(stack);
  await click(layer(heading));
  await click(button("Переместить"));
  expect(session.record.body.nodes[stack]!.children).toHaveLength(0);
  expect(session.record.body.nodes.n_root!.children).toContain(heading);
  session.manifest.components!.Required = {
    props: {
      title: { type: "string", required: true, minLength: 1 },
      choice: { type: "enum", required: true, values: ["a", "b"] },
    },
  };
  await act(async () => {
    await editor.refresh();
  });
  await change(component(), "Required");
  const panel = document.querySelector('[aria-label="Палитра"]')!;
  const title = [...panel.querySelectorAll<HTMLLabelElement>("label")]
    .find((l) => l.textContent === "title")!
    .querySelector("input")!;
  await click(add());
  expect(editor.store.getState().error).toBe("FIELD_INVALID");
  await change(title, "Ready");
  const choice = [...panel.querySelectorAll<HTMLSelectElement>("select")].find((s) =>
    s.closest("label")?.textContent?.startsWith("choice"),
  )!;
  await change(choice, "a");
  await click(add());
  expect(session.record.body.nodes[editor.store.getState().selected!]!.props).toEqual({
    title: "Ready",
    choice: "a",
  });
  const search = document.querySelector<HTMLInputElement>('[aria-label="Палитра"] input')!;
  await change(search, "required");
  expect(component().options).toHaveLength(1);
  await change(document.querySelector<HTMLSelectElement>('[aria-label="Язык"]')!, "en");
  expect(document.querySelector('[aria-label="Palette"]')).not.toBeNull();
  expect(document.body.textContent).toContain("To move a block, choose an insertion destination");
  expect(button("Duplicate").disabled).toBe(false);
});
it("STU-062: Delete/Ctrl+D действуют в дереве и сохраняют редактирование полей", async () => {
  const { api, session } = await setup(true);
  await click(layer("n_heading"));
  await act(async () => {
    layer("n_heading").dispatchEvent(
      new KeyboardEvent("keydown", { key: "d", ctrlKey: true, bubbles: true, cancelable: true }),
    );
  });
  await flush();
  expect(session.record.body.nodes.n_root!.children).toHaveLength(3);
  const id = editor.store.getState().selected!;
  await act(async () => {
    layer(id).dispatchEvent(
      new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }),
    );
  });
  await flush();
  expect(session.record.body.nodes[id]).toBeUndefined();
  await click(layer("n_custom"));
  const count = vi.mocked(api.apply).mock.calls.length;
  await act(async () => {
    input("property-title").dispatchEvent(
      new KeyboardEvent("keydown", { key: "d", ctrlKey: true, bubbles: true }),
    );
    input("property-title").dispatchEvent(
      new KeyboardEvent("keydown", { key: "Delete", bubbles: true }),
    );
  });
  expect(api.apply).toHaveBeenCalledTimes(count);
  await act(async () => {
    layer("n_custom").dispatchEvent(
      new KeyboardEvent("keydown", { key: "Delete", isComposing: true, bubbles: true }),
    );
  });
  expect(api.apply).toHaveBeenCalledTimes(count);
});
it("STU-061/064: нет целей в locked зоне, read-only и отказы сохраняют выбор", async () => {
  const { session, api } = await setup(true);
  await click(layer("n_heading"));
  vi.mocked(api.apply).mockRejectedValueOnce(new StudioError("FORBIDDEN"));
  await click(button("Удалить"));
  expect(editor.store.getState().selected).toBe("n_heading");
  expect(layer("n_heading")).toBeDefined();
  session.record.body.nodes.n_root!.zone = { id: "lock", mode: "SYSTEM", locked: true };
  await act(async () => {
    await editor.refresh();
  });
  for (const text of ["Добавить блок", "Дублировать", "Удалить", "Переместить"])
    expect(button(text).disabled).toBe(true);
  delete session.record.body.nodes.n_root!.zone;
  session.changeset.state = "approved";
  await act(async () => {
    await editor.refresh();
  });
  expect(button("Добавить блок").disabled).toBe(true);
});
it("STU-050/054: типы действий, другие страницы, конфликты/исключения и ru/en", async () => {
  const { session } = await setup(true);
  session.history.items = [
    action("edit", "node.setProps"),
    action("edit", "node.setDesign", "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c02"),
    action("edit", "unknown", "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c03"),
    action("redo", "node.rename", "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c04"),
  ];
  session.history.items[1]!.operations[0]!.target = changesetId;
  session.history.items[1]!.operations[0]!.status = "conflict";
  session.history.items[2]!.operations[0]!.status = "dropped";
  session.history.undoReason = "UNDO_NOT_SUPPORTED";
  await act(async () => {
    await editor.refresh();
  });
  await flush();
  for (const text of [
    "Изменение свойства",
    "Изменение оформления",
    "Есть изменения другого объекта",
    "Конфликт",
    "Исключено при переносе",
    "Это действие не поддерживает отмену.",
  ])
    expect(document.body.textContent).toContain(text);
  await change(document.querySelector<HTMLSelectElement>('[aria-label="Язык"]')!, "en");
  expect(document.body.textContent).toContain("This action does not support undo.");
  expect(document.body.textContent).toContain("Draft history");
  session.history.undoReason = "UNKNOWN_CODE";
  await act(async () => {
    await editor.refresh();
  });
  await flush();
  expect(document.body.textContent).toContain("Action unavailable");
});
it("STU-052: Ctrl/Cmd+Z/Shift+Z работают вне полей и не перехватывают локальную отмену", async () => {
  const { api, session } = await setup(true);
  session.history.canUndo = true;
  session.history.canRedo = true;
  session.history.undoReason = null;
  session.history.redoReason = null;
  await act(async () => {
    await editor.refresh();
  });
  await flush();
  const key = async (target: EventTarget, init: KeyboardEventInit) => {
    const event = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...init });
    await act(async () => {
      target.dispatchEvent(event);
    });
    await flush();
    return event;
  };
  expect((await key(window, { key: "z", ctrlKey: true })).defaultPrevented).toBe(true);
  expect(vi.mocked(api.changeHistory).mock.calls.at(-1)![1]).toBe("undo");
  await key(window, { key: "я", code: "KeyZ", metaKey: true, shiftKey: true });
  expect(vi.mocked(api.changeHistory).mock.calls.at(-1)![1]).toBe("redo");
  await click(layer("n_heading"));
  expect((await key(input("property-text"), { key: "z", ctrlKey: true })).defaultPrevented).toBe(
    false,
  );
  const editable = document.createElement("div");
  editable.contentEditable = "true";
  editable.setAttribute("contenteditable", "true");
  document.body.append(editable);
  await key(editable, { key: "z", ctrlKey: true });
  await key(window, { key: "z" });
  await key(window, { key: "z", ctrlKey: true, altKey: true });
  await key(window, { key: "z", ctrlKey: true, isComposing: true });
  expect(api.changeHistory).toHaveBeenCalledTimes(2);
  await act(async () => {
    editor.store.setState({ status: "saving" });
  });
  await key(window, { key: "z", ctrlKey: true });
  expect(api.changeHistory).toHaveBeenCalledTimes(2);
  await act(async () => {
    editor.store.setState({ status: "ready" });
  });
  await click(button("Отменить"));
  await click(button("Повторить"));
  expect(api.changeHistory).toHaveBeenCalledTimes(4);
});
it("STU-053: неизвестный исход блокирует кнопки до проверки того же запроса", async () => {
  const { api, session } = await setup(true);
  session.history.canUndo = true;
  session.history.undoReason = null;
  await act(async () => {
    await editor.refresh();
  });
  await flush();
  vi.mocked(api.changeHistory).mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true));
  await click(button("Отменить"));
  await flush();
  expect(button("Отменить").disabled).toBe(true);
  await click(button("Проверить отмену или повтор"));
  await flush();
  expect(vi.mocked(api.changeHistory).mock.calls[0]).toEqual(
    vi.mocked(api.changeHistory).mock.calls[1],
  );
  expect(button("Отменить").disabled).toBe(false);
});
async function selectPage() {
  await change(
    document.querySelector<HTMLSelectElement>('.open-bar select[aria-label="Страница"]')!,
    documentId,
  );
  await flush();
}
it("STU-042/044: созданный CS остаётся выбранным за первой страницей списка", async () => {
  const { api } = await setup();
  await selectPage();
  vi.mocked(api.drafts).mockResolvedValue({ items: [], nextCursor: changesetId });
  await click(button("Новый черновик"));
  await change(
    document.querySelector<HTMLInputElement>('[aria-label="Название черновика"]')!,
    "New",
  );
  await submit(document.querySelector<HTMLFormElement>(".create-bar")!);
  expect(document.querySelector<HTMLSelectElement>('[aria-label="Мой черновик"]')!.value).toBe(
    changesetId,
  );
  expect(button("Открыть").disabled).toBe(false);
});
it("STU-043: два submit до ответа выполняют одну команду создания", async () => {
  const { api, session } = await setup();
  await selectPage();
  await click(button("Новый черновик"));
  await change(
    document.querySelector<HTMLInputElement>('[aria-label="Название черновика"]')!,
    "New",
  );
  let finish!: () => void;
  vi.mocked(api.createDraft).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = () => resolve(session.changeset);
      }),
  );
  const form = document.querySelector<HTMLFormElement>(".create-bar")!;
  await submit(form);
  await submit(form);
  expect(api.createDraft).toHaveBeenCalledTimes(1);
  await act(async () => {
    finish();
  });
  await flush();
  expect(editor.store.getState().status).toBe("ready");
});
it("STU-040/041/044: выбор без ID, смена страницы сбрасывает черновик и не открывает автоматически", async () => {
  const { api, session } = await setup();
  expect(document.querySelector(".open-bar input")).toBeNull();
  expect(document.body.textContent).toContain("/store · Store");
  await selectPage();
  const picker = document.querySelector<HTMLSelectElement>('[aria-label="Мой черновик"]')!;
  await change(picker, changesetId);
  expect(button("Открыть").disabled).toBe(false);
  expect(api.open).not.toHaveBeenCalled();
  await submit(document.querySelector<HTMLFormElement>(".open-bar")!);
  expect(window.location.hash).toContain(documentId);
  const second = "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c33";
  vi.mocked(api.pages).mockResolvedValue({
    items: [
      { id: documentId, path: "/store", title: "", hasHead: true },
      { id: second, path: "/second", title: "Second", hasHead: true },
    ],
    nextCursor: null,
  });
  await click(button("Обновить списки"));
  await flush();
  await change(document.querySelector<HTMLSelectElement>('select[aria-label="Страница"]')!, second);
  await flush();
  expect(picker.value).toBe("");
  expect(button("Открыть").disabled).toBe(true);
  expect(editor.store.getState().session?.record.id).toBe(session.record.id);
});
it("STU-042/043: потерянный ответ создания повторяет тот же запрос, затем открывает созданный CS", async () => {
  const { api } = await setup();
  await selectPage();
  await click(button("Новый черновик"));
  await change(
    document.querySelector<HTMLInputElement>('[aria-label="Название черновика"]')!,
    "Работа над страницей",
  );
  vi.mocked(api.createDraft).mockRejectedValueOnce(new StudioError("NETWORK_UNCERTAIN", true));
  await submit(document.querySelector<HTMLFormElement>(".create-bar")!);
  expect(document.body.textContent).toContain("второй черновик не создастся");
  expect(button("Новый черновик").disabled).toBe(true);
  expect(button("Отмена").disabled).toBe(true);
  await submit(document.querySelector<HTMLFormElement>(".create-bar")!);
  const calls = vi.mocked(api.createDraft).mock.calls;
  expect(calls).toHaveLength(2);
  expect(calls[0]).toEqual(calls[1]);
  expect(calls[0]![0]).toBe("Работа над страницей");
  expect(api.open).toHaveBeenCalledWith(documentId, changesetId, expect.any(AbortSignal));
  expect(document.querySelector(".create-bar")).toBeNull();
});
it("STU-043: отказ открытия после создания повторяет открытие, не создание", async () => {
  const { api } = await setup();
  await selectPage();
  await click(button("Новый черновик"));
  await change(
    document.querySelector<HTMLInputElement>('[aria-label="Название черновика"]')!,
    "New",
  );
  vi.mocked(api.open).mockRejectedValueOnce(new StudioError("NOT_FOUND"));
  await submit(document.querySelector<HTMLFormElement>(".create-bar")!);
  expect(document.body.textContent).toContain("Не удалось открыть черновик");
  expect(button("Открыть созданный черновик")).toBeDefined();
  await submit(document.querySelector<HTMLFormElement>(".create-bar")!);
  expect(api.createDraft).toHaveBeenCalledTimes(1);
  expect(api.open).toHaveBeenCalledTimes(2);
});
it("STU-044: пустые списки, отказ чтения/создания и повтор доступны на обоих языках", async () => {
  const { api } = await setup();
  await selectPage();
  vi.mocked(api.drafts).mockResolvedValue({ items: [], nextCursor: null });
  await click(button("Обновить списки"));
  await flush();
  expect(document.body.textContent).toContain("У вас нет открытых черновиков");
  await click(button("Новый черновик"));
  await click(button("Отмена"));
  expect(document.querySelector(".create-bar")).toBeNull();
  await click(button("Новый черновик"));
  await change(
    document.querySelector<HTMLInputElement>('[aria-label="Название черновика"]')!,
    "New",
  );
  vi.mocked(api.createDraft).mockRejectedValueOnce(new StudioError("FORBIDDEN"));
  await submit(document.querySelector<HTMLFormElement>(".create-bar")!);
  expect(document.body.textContent).toContain("Не удалось создать черновик");
  await change(document.querySelector<HTMLSelectElement>('[aria-label="Язык"]')!, "en");
  expect(document.body.textContent).toContain("Could not create draft");
  await change(document.querySelector<HTMLSelectElement>('[aria-label="Language"]')!, "ru");
  expect(button("Отмена").disabled).toBe(false);
  await click(button("Отмена"));
  vi.mocked(api.pages).mockRejectedValueOnce(Error("secret"));
  await click(button("Обновить списки"));
  await flush();
  expect(document.body.textContent).toContain("Не удалось загрузить список");
  expect(document.body.textContent).not.toContain("secret");
  vi.mocked(api.pages).mockResolvedValue({ items: [], nextCursor: null });
  await click(button("Обновить списки"));
  await flush();
  expect(document.body.textContent).toContain("Нет доступных страниц");
  await change(document.querySelector<HTMLSelectElement>('[aria-label="Язык"]')!, "en");
  expect(document.body.textContent).toContain("No available pages");
});
it("STU-040/044: пагинация обоих списков и страница только в CS", async () => {
  const { api, session } = await setup();
  vi.mocked(api.pages)
    .mockResolvedValueOnce({
      items: [{ id: documentId, path: "/store", title: "Draft only", hasHead: false }],
      nextCursor: documentId,
    })
    .mockResolvedValue({ items: [], nextCursor: null });
  await click(button("Обновить списки"));
  await flush();
  await selectPage();
  expect(button("Новый черновик").disabled).toBe(true);
  expect(document.body.textContent).toContain("Страница существует только в черновике");
  await click(button("Ещё страницы"));
  await flush();
  expect(api.pages).toHaveBeenLastCalledWith(documentId, expect.any(AbortSignal));
  vi.mocked(api.drafts)
    .mockResolvedValueOnce({ items: [session.changeset], nextCursor: changesetId })
    .mockResolvedValue({ items: [], nextCursor: null });
  // Refresh each cached page; return a cursor for the first draft page.
  vi.mocked(api.pages).mockResolvedValue({
    items: [{ id: documentId, path: "/store", title: "Draft only", hasHead: false }],
    nextCursor: null,
  });
  await click(button("Обновить списки"));
  await flush();
  await click(button("Ещё черновики"));
  await flush();
  expect(api.drafts).toHaveBeenLastCalledWith(documentId, changesetId, expect.any(AbortSignal));
});
it("STU-001/003/010/021/026/033: открывает, выбирает, сохраняет текст/числа/токены, меняет локаль", async () => {
  const { api, session } = await setup();
  session.record.body.nodes.n_root!.type = "Stack";
  expect(document.body.textContent).toContain("Откройте страницу");
  const ids = document.querySelectorAll<HTMLSelectElement>(".open-bar select");
  await change(ids[0]!, documentId);
  await flush();
  await change(ids[1]!, changesetId);
  await submit(document.querySelector<HTMLFormElement>(".open-bar")!);
  expect(api.open).toHaveBeenCalled();
  expect(document.querySelector("iframe")).not.toBeNull();
  await click(layer("n_heading"));
  expect(layer("n_heading").getAttribute("aria-selected")).toBe("true");
  await change(input("property-level"), "3");
  await submit(input("property-level").closest("form")!);
  expect(session.record.body.nodes.n_heading!.props).toEqual({ level: 3 });
  await change(input("property-text"), "Текст из Studio");
  await submit(input("property-text").closest("form")!);
  expect(Object.values(session.record.body.localContent!)[0]!.value).toEqual({
    ru: "Текст из Studio",
  });
  expect(input("design-padding")).toBeNull();
  await click(layer("n_root"));
  await change(input("design-padding"), "12");
  await submit(input("design-padding").closest("form")!);
  expect(session.record.body.nodes.n_root!.design).toEqual({ padding: { raw: 12 } });
  const point = document.querySelector<HTMLSelectElement>('select[aria-label="Размер экрана"]')!;
  await change(point, "sm");
  await change(input("design-padding"), "16");
  await submit(input("design-padding").closest("form")!);
  expect(session.record.body.nodes.n_root!.design).toEqual({
    padding: { base: { raw: 12 }, sm: { raw: 16 } },
  });
  await change(input("design-gap"), "sm");
  await submit(input("design-gap").closest("form")!);
  await click(layer("n_custom"));
  await change(document.getElementById("property-enabled") as HTMLSelectElement, "true");
  await submit(input("property-enabled").closest("form")!);
  expect(session.record.body.nodes.n_custom!.props).toMatchObject({ enabled: true });
  await change(document.getElementById("property-choice") as HTMLSelectElement, "two");
  await submit(input("property-choice").closest("form")!);
  await change(input("property-title"), "Edited");
  await submit(input("property-title").closest("form")!);
  expect(session.record.body.nodes.n_custom!.props).toMatchObject({
    choice: "two",
    title: "Edited",
  });
  await change(input("property-title"), "");
  await submit(input("property-title").closest("form")!);
  expect(document.querySelector("[role=alert]")!.textContent).toContain("FIELD_INVALID");
  await change(input("property-title"), "Edited");
  await click(layer("n_root"));
  await change(input("design-gap"), "-1");
  await submit(input("design-gap").closest("form")!);
  expect(editor.store.getState().error).toBe("FIELD_INVALID");
  await change(input("design-gap"), "sm");
  await change(document.querySelector<HTMLSelectElement>('select[aria-label="Язык"]')!, "en");
  expect(document.documentElement.lang).toBe("en");
  expect(document.body.textContent).toContain("Properties");
  await change(document.querySelector<HTMLSelectElement>(".canvas-tools label select")!, "375");
  expect(editor.store.getState().width).toBe(375);
  await click(button("Interact"));
  expect(editor.store.getState().mode).toBe("interact");
  await click(button("Design"));
  await click(button("Reload draft"));
  expect(editor.store.getState().status).toBe("ready");
});
it("STU-032: клавиатурное дерево, рамки и instance синхронизированы с iframe", async () => {
  const { session } = await setup(true);
  await flush();
  const keys = [
    "ArrowRight",
    "ArrowDown",
    "ArrowUp",
    "End",
    "Home",
    "ArrowRight",
    "ArrowLeft",
    "ArrowRight",
    "Escape",
  ];
  for (const key of keys)
    await act(async () => {
      document
        .querySelector("[role=tree]")!
        .dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
    });
  expect(editor.store.getState().selected).toBe("n_root");
  const frame = document.querySelector("iframe")!;
  await act(async () => {
    frame.dispatchEvent(new Event("load"));
  });
  const send = (type: string, payload: Record<string, unknown>) =>
    window.dispatchEvent(
      new MessageEvent("message", {
        origin: session.config.appOrigin,
        source: frame.contentWindow,
        data: { cms: "preview", v: 1, type, payload },
      }),
    );
  await act(async () => {
    send("ready", { documentId, protocolVersion: 1 });
    send("ready", { documentId, protocolVersion: 1 });
    send("node.click", { nodeId: "n_heading", instance: "repeat/one" });
    send("layout", {
      nodes: {
        n_heading: [
          { x: 10, y: 20, w: 100, h: 30, instance: "repeat/one" },
          { x: 10, y: 100, w: 100, h: 30, instance: "repeat/two" },
        ],
      },
    });
  });
  expect(document.querySelectorAll(".selection-frame")).toHaveLength(1);
  expect(document.activeElement).toBe(layer("n_heading"));
  expect(document.body.textContent).toContain("Экземпляр повторения");
  expect(document.body.textContent).toContain("Предпросмотр подключён");
  await click(layer("n_heading"));
  expect(document.querySelectorAll(".selection-frame")).toHaveLength(2);
  await click(button("Взаимодействие"));
  expect(document.querySelectorAll(".selection-frame")).toHaveLength(0);
  await act(async () => {
    editor.store.setState({ previewError: "CMS_PREVIEW_UPDATE_FAILED" });
  });
  expect(document.querySelector(".preview-error")!.textContent).toContain(
    "CMS_PREVIEW_UPDATE_FAILED",
  );
  await click(button("Повторить предпросмотр"));
  expect(editor.store.getState().previewError).toBeNull();
});
it("STU-021: read-only и неопределённый исход видны, данные не записываются", async () => {
  const { session } = await setup(true);
  session.changeset.state = "submitted";
  await act(async () => {
    await editor.refresh();
  });
  expect(document.body.textContent).toContain("доступен только для чтения");
  await click(layer("n_heading"));
  expect(button("Сохранить level").disabled).toBe(true);
  await act(async () => {
    editor.store.setState({
      status: "uncertain",
      error: "NETWORK_UNCERTAIN",
      instance: "repeat/x",
    });
  });
  expect(document.body.textContent).toContain("Исход сохранения неизвестен");
  expect(button("Открыть").disabled).toBe(true);
  await act(async () => {
    editor.store.setState({ selected: null, document: null });
  });
  expect(document.body.textContent).toContain("Выберите элемент");
});
it("ошибки открытия/config и блокировка узла отображаются безопасно", async () => {
  const { api, session } = await setup();
  vi.mocked(api.config).mockRejectedValue(Error("credential"));
  await act(async () => {
    await client.invalidateQueries({ queryKey: ["studio-config"] });
  });
  await flush();
  expect(document.body.textContent).toContain("STUDIO_CONFIG_UNAVAILABLE");
  expect(document.body.textContent).not.toContain("credential");
  session.record.body.nodes.n_root!.locked = true;
  await act(async () => {
    await editor.open(documentId, changesetId);
  });
  expect(document.querySelector('[aria-label="Элемент заблокирован"]')).not.toBeNull();
  vi.mocked(api.open).mockRejectedValue(Error("secret"));
  await act(async () => {
    await editor.open(documentId, changesetId);
  });
  expect(document.body.textContent).toContain("OPEN_FAILED");
});
