// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { DataSourcesPanel } from "../src/data-sources-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
import { dataFixture } from "./data-sources-fixture.js";
import { resultFields, repeatTree, insertRepeat } from "../src/data-sources.js";
import { applyOperations } from "../src/operations.js";
import { StudioError } from "../src/types.js";
let root: Root, editor: Editor;
it("STU-220/235: a seq conflict retains the source form input without a blind retry", async () => {
  const { api } = await setup();
  await click("Настроить featured");
  await change("Значение параметра limit", "literal");
  await change("Литерал limit", "4");
  vi.mocked(api.apply).mockRejectedValueOnce(new StudioError("CHANGESET_SEQ_CONFLICT"));
  await click("Сохранить данные");
  expect(document.querySelector<HTMLInputElement>('[aria-label="Литерал limit"]')!.value).toBe("4");
  expect(api.apply).toHaveBeenCalledTimes(1);
  expect(editor.store.getState().error).toBe("CHANGESET_SEQ_CONFLICT");
  await click("Отмена");
});
it("STU-234: an open form can be discarded after edit access is revoked", async () => {
  await setup();
  await click("Подключить источник");
  await act(async () => {
    const session = editor.store.getState().session!;
    editor.store.setState({
      session: { ...session, review: { ...session.review, canEdit: false } },
    });
  });
  expect(button("Отмена").disabled).toBe(false);
  await click("Отмена");
  expect(editor.store.getState().dirtyFields["property-dataSource"]).toBe(false);
});
it("STU-228/234: revoked edit access still permits discarding Repeat settings without a write", async () => {
  const { source, repeat } = repeatedFixture();
  const { api } = await setup(source, undefined, repeat.id);
  await change("Лимит повторения", "7");
  expect(editor.store.getState().dirtyFields["property-repeatSettings"]).toBe(true);
  await act(async () => {
    const session = editor.store.getState().session!;
    editor.store.setState({
      session: { ...session, review: { ...session.review, canEdit: false } },
    });
  });
  expect(button("Сохранить повторение").matches(":disabled")).toBe(true);
  expect(button("Сбросить").matches(":disabled")).toBe(false);
  await click("Сбросить");
  expect(editor.store.getState().dirtyFields["property-repeatSettings"]).toBe(false);
  expect(api.apply).not.toHaveBeenCalled();
});
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const button = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === name)!;
async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}
async function click(name: string) {
  await act(async () => button(name).click());
  await flush();
}
async function change(label: string, value: string) {
  const input = [...document.querySelectorAll("label")]
    .find((l) => l.textContent?.startsWith(label))!
    .querySelector("input,select") as HTMLInputElement | HTMLSelectElement;
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      input.tagName === "SELECT" ? HTMLSelectElement.prototype : HTMLInputElement.prototype,
      "value",
    )!.set!.call(input, value);
    input.dispatchEvent(new Event("change", { bubbles: true }));
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await flush();
}
async function check(label: string) {
  const input = [...document.querySelectorAll("label")]
    .find((l) => l.textContent?.includes(label))!
    .querySelector<HTMLInputElement>('input[type="checkbox"]')!;
  await act(async () => input.click());
  await flush();
}
async function setup(source = dataFixture(), rights?: string[], selected = "n_heading") {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api, session } = mockApi(source);
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select(selected);
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<DataSourcesPanel {...{ editor, rights }} />));
  await flush();
  return { api, session };
}
function repeatedFixture() {
  const source = dataFixture(),
    doc = source.record.body;
  const list = resultFields(doc, source.manifest).fields.find(
    (f) => f.path === "$data.featured.items",
  )!;
  const tree = repeatTree(list, 12, "$item.id", new Set(Object.keys(doc.nodes)));
  source.record.body = applyOperations(doc, [
    insertRepeat(source.record.id, { parentId: doc.root, slot: "", index: 1 }, tree),
  ]);
  const repeat = source.record.body.nodes[tree.id!]!,
    template = source.record.body.nodes[repeat.children![0]!]!;
  template.children = ["n_item"];
  source.record.body.nodes.n_item = { id: "n_item", type: "Text" };
  return { source, repeat };
}
it("STU-219/222/235: catalogue, parameter modes, defaults, invalid key and explicit save", async () => {
  const { api } = await setup();
  expect(document.body.textContent).toContain("Первая порция");
  expect(document.body.textContent).toContain("Обязательный параметр");
  await click("Подключить источник");
  expect(editor.store.getState().dirtyFields["property-dataSource"]).toBe(true);
  await change("Ключ подключения", "content");
  expect(button("Сохранить данные").disabled).toBe(true);
  await change("Ключ подключения", "featured2");
  await change("Значение параметра collection", "path");
  await change("Путь параметра collection", "$content.handle");
  await change("Значение параметра limit", "literal");
  await change("Литерал limit", "201");
  expect(button("Сохранить данные").disabled).toBe(true);
  await change("Литерал limit", "4");
  await change("Значение параметра enabled", "literal");
  await change("Литерал enabled", "false");
  await change("Значение параметра choice", "literal");
  await change("Литерал choice", "one");
  await change("Значение параметра enabled", "null");
  expect(button("Сохранить данные").disabled).toBe(false);
  await click("Сохранить данные");
  expect(editor.store.getState().document!.dataSources!.featured2).toMatchObject({
    source: "test.items",
    params: {
      collection: "$content.handle",
      limit: { lit: 4 },
      enabled: { lit: null },
      choice: { lit: "one" },
    },
  });
  expect(api.apply).toHaveBeenCalledTimes(1);
});
it("STU-225/226/228: create Repeat, choose index/limit, save settings and preserve one template", async () => {
  await setup();
  await click("Добавить повторение");
  await change("Предел элементов", "0");
  expect(button("Сохранить данные").disabled).toBe(true);
  await change("Предел элементов", "4");
  await change("Ключ элемента", "");
  expect(document.body.textContent).toContain("Индекс меняет идентичность");
  await click("Сохранить данные");
  const doc = editor.store.getState().document!,
    id = editor.store.getState().selected!,
    repeat = doc.nodes[id]!;
  expect(repeat.type).toBe("Repeat");
  expect(repeat.children).toHaveLength(1);
  expect(repeat.slots!.empty).toHaveLength(1);
  await change("Лимит повторения", "7");
  await click("Сбросить");
  await change("Лимит повторения", "6");
  await change("Ключ повторения", "$item.id");
  await click("Сохранить повторение");
  expect(editor.store.getState().document!.nodes[id]!.props).toMatchObject({
    limit: 6,
    key: "$item.id",
  });
});
it("STU-224/227: typed item binding, explicit replacement/clear and Escape returns focus", async () => {
  const { source } = repeatedFixture();
  await setup(source, undefined, "n_item");
  await click("Привязать данные к свойству");
  await change("Свойство блока", "text");
  await change("Поле результата или элемента", "$item.title");
  await click("Сохранить данные");
  expect(editor.store.getState().document!.nodes.n_item!.bindings!.text).toBe("$item.title");
  await click("Привязать данные к свойству");
  await change("Свойство блока", "text");
  await check("Снять привязку");
  await check("Подтверждаю замену или снятие");
  await click("Сохранить данные");
  expect(editor.store.getState().document!.nodes.n_item!.bindings).toBeUndefined();
  await click("Подключить источник");
  await act(async () =>
    document
      .querySelector(".content-bindings > fieldset")!
      .dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })),
  );
  expect(editor.store.getState().dirtyFields["property-dataSource"]).toBe(false);
  expect(document.activeElement).toBe(button("Подключить источник"));
});
it("STU-229: dependency confirmation clears supported bindings atomically; complex paths block", async () => {
  const { source, repeat } = repeatedFixture();
  source.record.body.nodes.n_item!.bindings = { text: "$item.title" };
  const { api } = await setup(source);
  await click("Настроить featured");
  await check("Удалить подключение");
  expect(button("Сохранить данные").disabled).toBe(true);
  expect(document.body.textContent).toContain(`/nodes/${repeat.id}/bindings/items`);
  await check("Подтверждаю изменение зависимостей");
  await click("Сохранить данные");
  expect(editor.store.getState().document!.dataSources).toBeUndefined();
  expect(api.apply).toHaveBeenCalledTimes(1);
});
it("STU-234/235: read-only, English labels, unknown source and unsupported contract stay readable", async () => {
  const source = dataFixture();
  source.record.body.dataSources!.unknown = { source: "unavailable" };
  source.record.body.dataSources!.content = { source: "test.pages" };
  await setup(source, ["design.read"]);
  expect(button("Подключить источник").disabled).toBe(true);
  expect(button("Настроить content").disabled).toBe(true);
  expect(document.body.textContent).toContain("Источник отсутствует");
  await act(async () => editor.locale("en"));
  expect(button("Connect source").disabled).toBe(true);
  expect(document.body.textContent).toContain("unsupported editor");
});
it("STU-219/230: no registered sources is a readable state and configuration never invokes app code", async () => {
  const source = dataFixture();
  delete source.manifest.dataSources;
  delete source.record.body.dataSources;
  const { api } = await setup(source);
  expect(button("Подключить источник").disabled).toBe(true);
  expect(document.body.textContent).toContain("нет зарегистрированных");
  expect(api.apply).not.toHaveBeenCalled();
});
it("STU-222/229: unsupported legacy operands remain read only while an adjacent parameter changes", async () => {
  const source = dataFixture();
  source.manifest.dataSources!["test.items"]!.params!.record = {
    type: "object",
    required: true,
    fields: { name: { type: "string" } },
  };
  source.record.body.dataSources!.featured!.params!.record = { lit: { name: "preserved" } };
  await setup(source);
  await click("Настроить featured");
  expect(document.body.textContent).toContain("Выражение сохраняется без изменений");
  await change("Значение параметра limit", "literal");
  await change("Литерал limit", "4");
  await click("Сохранить данные");
  expect(editor.store.getState().document!.dataSources!.featured!.params!.record).toEqual({
    lit: { name: "preserved" },
  });
});
