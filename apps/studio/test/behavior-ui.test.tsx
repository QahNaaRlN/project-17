// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { BehaviorPanel } from "../src/behavior-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId, persist } from "./fixture.js";
import { dataFixture } from "./data-sources-fixture.js";
import { StudioError } from "../src/types.js";
let root: Root, editor: Editor;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const buttons = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].filter(
    (b) => b.textContent?.trim() === name,
  );
const button = (name: string) => buttons(name)[0]!;
async function click(name: string) {
  await act(async () => button(name).click());
}
function input(label: string, index = 0) {
  const labels = [...document.querySelectorAll("label")].filter((l) =>
    l.textContent?.startsWith(label),
  );
  return labels[index]!.querySelector("input,select") as HTMLInputElement | HTMLSelectElement;
}
async function change(label: string, value: string, index = 0) {
  const el = input(label, index);
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      el.tagName === "SELECT" ? HTMLSelectElement.prototype : HTMLInputElement.prototype,
      "value",
    )!.set!.call(el, value);
    el.dispatchEvent(new Event("change", { bubbles: true }));
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
function source() {
  const s = dataFixture();
  s.record.body.nodes.n_heading!.type = "Button";
  s.record.body.localContent = { title: { type: "text", value: { ru: "RU", en: "EN" } } };
  s.manifest.actions = {
    "test.use": {
      description: "Typed action",
      args: {
        name: { type: "string", required: true },
        enabled: { type: "boolean" },
        count: { type: "number", default: 0 },
        object: { type: "object", fields: {} },
      },
    },
    "test.empty": {},
    "test.unavailable": { args: { ref: { type: "reference", required: true, schema: "Absent" } } },
  };
  return s;
}
async function setup(s = source(), rights?: string[], selected = "n_heading") {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const { api, session } = mockApi(s);
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select(selected);
  document.body.innerHTML = "<div id='root'></div>";
  root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<BehaviorPanel {...{ editor, rights }} />));
  return { api, session };
}
it("STU-237/238/239/240/243: typed action chains save in order without executing handlers", async () => {
  const { api, session } = await setup();
  await click("Настроить событие click");
  await change("Действие", "test.use");
  await click("Добавить действие");
  expect(document.body.textContent).toContain("Typed action");
  expect(button("Сохранить").disabled).toBe(true);
  await change("Значение параметра name", "path");
  await change("Путь параметра name", "$context.locale");
  await change("Значение параметра enabled", "literal");
  await change("Литерал enabled", "false");
  await change("Значение параметра count", "literal");
  await change("Литерал count", "0");
  await change("Действие", "test.empty");
  await click("Добавить действие");
  await click("Выше 2");
  await click("Ниже 1");
  await click("Удалить действие 2");
  await click("Сохранить");
  expect(api.apply).toHaveBeenCalledTimes(1);
  expect(session.record.body.nodes.n_heading!.on!.click).toEqual([
    {
      action: "test.use",
      args: { name: "$context.locale", enabled: { lit: false }, count: { lit: 0 } },
    },
  ]);
  expect(document.querySelector("form")).toBeNull();
  await click("Настроить событие click");
  await click("Удалить действие 1");
  await click("Сохранить");
  expect(session.record.body.nodes.n_heading!.on).toBeUndefined();
});
it("STU-242/253: URL, anchor and scroll targets are selected from document nodes", async () => {
  const { session } = await setup();
  await click("Настроить событие click");
  await click("Добавить действие");
  await change("Источник аргумента to", "url");
  await change("URL to", "javascript:bad");
  expect(button("Сохранить").disabled).toBe(true);
  await change("URL to", "https://example.test");
  expect(button("Сохранить").disabled).toBe(false);
  await change("Источник аргумента to", "anchor");
  await change("Целевой узел to", "n_heading");
  await change("Действие", "scrollTo");
  await click("Добавить действие");
  await change("Источник аргумента target", "target");
  await change("Целевой узел target", "n_root");
  await click("Сохранить");
  expect(session.record.body.nodes.n_heading!.on!.click).toEqual([
    { action: "navigate", args: { to: { lit: { kind: "anchor", node: "n_heading" } } } },
    { action: "scrollTo", args: { target: { lit: "n_root" } } },
  ]);
});
it("STU-239/245/253: reordered steps and removed predicates keep displayed literals aligned with saved values", async () => {
  const { session } = await setup();
  await click("Настроить событие click");
  await change("Действие", "test.use");
  await click("Добавить действие");
  await change("Значение параметра name", "literal");
  await change("Литерал name", "One");
  await click("Добавить действие");
  await change("Значение параметра name", "literal", 1);
  await change("Литерал name", "Two", 1);
  await click("Выше 2");
  expect(input("Литерал name", 0).value).toBe("Two");
  expect(input("Литерал name", 1).value).toBe("One");
  await click("Сохранить");
  expect(session.record.body.nodes.n_heading!.on!.click).toMatchObject([
    { args: { name: { lit: "Two" } } },
    { args: { name: { lit: "One" } } },
  ]);
  await click("Настроить условие");
  await change("Оператор", "eq");
  await change("Поле условия", "$context.locale");
  await change("Значение параметра Сравнение", "literal");
  await change("Литерал Сравнение", "ru");
  await change("Оператор", "and");
  await click("Добавить условие");
  await change("Оператор", "eq", 2);
  await change("Поле условия", "$context.locale", 1);
  await change("Значение параметра Сравнение", "literal", 1);
  await change("Литерал Сравнение", "en", 1);
  await click("Удалить условие 1");
  expect(input("Литерал Сравнение").value).toBe("en");
  await click("Сохранить");
  expect(session.record.body.nodes.n_heading!.when).toEqual({
    op: "and",
    args: [{ op: "eq", args: ["$context.locale", { lit: "en" }] }],
  });
});
it("STU-244/245/246: conditions support groups, not, typed comparison, null and homogeneous in lists", async () => {
  const { session } = await setup();
  await click("Настроить условие");
  await change("Оператор", "eq");
  await change("Поле условия", "$context.locale");
  await change("Значение параметра Сравнение", "literal");
  await change("Литерал Сравнение", "ru");
  await change("Оператор", "and");
  await click("Добавить условие");
  await change("Оператор", "not", 2);
  await change("Оператор", "empty", 3);
  await click("Удалить условие 2");
  await click("Сохранить");
  expect(session.record.body.nodes.n_heading!.when).toEqual({
    op: "and",
    args: [{ op: "eq", args: ["$context.locale", { lit: "ru" }] }],
  });
  await click("Настроить условие");
  await click("Снять");
  expect(session.record.body.nodes.n_heading!.when).toBeUndefined();
  await click("Настроить условие");
  await change("Оператор", "in");
  await change("Поле условия", "$context.locale");
  await click("Добавить значение");
  await change("Значение параметра 1", "literal");
  await change("Литерал 1", "ru");
  await click("Добавить значение");
  await change("Значение параметра 2", "literal");
  await change("Литерал 2", "en");
  await click("Удалить значение 2");
  await click("Сохранить");
  expect(session.record.body.nodes.n_heading!.when).toEqual({
    op: "in",
    args: ["$context.locale", { lit: ["ru"] }],
  });
  await click("Настроить условие");
  await change("Оператор", "neq");
  await change("Поле условия", "$context.locale");
  await change("Значение параметра Сравнение", "null");
  await click("Сохранить");
  expect(session.record.body.nodes.n_heading!.when).toEqual({
    op: "neq",
    args: ["$context.locale", { lit: null }],
  });
});
it("STU-243/251/253: conflict retains input and dirty guard blocks locale, mode and selection", async () => {
  const { api } = await setup();
  await click("Настроить событие click");
  await change("Действие", "test.empty");
  await click("Добавить действие");
  await act(async () => {
    editor.locale("en");
    editor.mode("interact");
    editor.select("n_root");
  });
  expect(editor.store.getState()).toMatchObject({
    locale: "ru",
    mode: "design",
    selected: "n_heading",
  });
  vi.mocked(api.apply).mockRejectedValueOnce(new StudioError("CHANGESET_SEQ_CONFLICT"));
  await click("Сохранить");
  expect(editor.store.getState().error).toBe("CHANGESET_SEQ_CONFLICT");
  expect(document.querySelector("form")).not.toBeNull();
  await act(async () => {
    const s = editor.store.getState().session!;
    editor.store.setState({ session: { ...s, review: { ...s.review, canEditBehavior: false } } });
  });
  expect(button("Сохранить").disabled).toBe(true);
  expect(button("Сбросить ввод").disabled).toBe(false);
  await click("Сбросить ввод");
  expect(editor.store.getState().dirtyFields["property-behavior"]).toBe(false);
  expect(api.apply).toHaveBeenCalledTimes(1);
});
it("STU-243: lost save response retries the frozen operation, seq and key", async () => {
  const { api, session } = await setup();
  const calls: unknown[][] = [];
  vi.mocked(api.apply).mockImplementation(async (...args) => {
    calls.push(structuredClone(args.slice(0, 3)));
    if (calls.length === 1) {
      persist(session, args[1]);
      throw new TypeError("network");
    }
  });
  await click("Настроить событие click");
  await change("Действие", "test.empty");
  await click("Добавить действие");
  await click("Сохранить");
  expect(editor.store.getState().bindingRetry).toBe(true);
  await act(async () => editor.retryBinding());
  expect(calls).toHaveLength(2);
  expect(calls[0]).toEqual(calls[1]);
});
it("STU-251: behavior-only actor can edit on while design/when remain closed, missing flags fail closed", async () => {
  const s = source();
  s.review.canEdit = false;
  s.review.canEditCondition = false;
  const { api } = await setup(s, ["design.read", "behavior.use", "content.read"]);
  expect(button("Настроить условие").disabled).toBe(true);
  await click("Настроить событие click");
  await change("Действие", "test.empty");
  await click("Добавить действие");
  await click("Сохранить");
  expect(api.apply).toHaveBeenCalledTimes(1);
  await act(async () => {
    const session = editor.store.getState().session!;
    delete session.review.canEditBehavior;
    editor.store.setState({ session: { ...session } });
  });
  expect(button("Настроить событие click").disabled).toBe(true);
});
it("STU-252: unknown event and legacy whole predicates require explicit replacement/removal", async () => {
  const s = source();
  s.record.body.nodes.n_heading!.on = {
    unknown: { action: "pending:missing", args: { value: "$event.payload" } },
  };
  s.record.body.nodes.n_heading!.when = { op: "gt", args: ["$props.date", { lit: "2026-01-01" }] };
  const { session, api } = await setup(s);
  await click("Настроить событие unknown");
  expect(document.body.textContent).toContain("только для чтения");
  expect(button("Снять").disabled).toBe(true);
  await act(async () => input("Подтверждаю").click());
  expect(button("Заменить выражение").disabled).toBe(true);
  await click("Снять");
  expect(session.record.body.nodes.n_heading!.on).toBeUndefined();
  await click("Настроить условие");
  await act(async () => input("Подтверждаю").click());
  await click("Заменить выражение");
  await click("Сохранить");
  expect(api.apply).toHaveBeenCalledTimes(2);
});
it("STU-237/251/253: eventless, locked ancestor, English and Escape states", async () => {
  const s = source();
  s.record.body.nodes.n_heading!.locked = true;
  await setup(s, ["design.compose", "behavior.use", "content.read"], "n_root");
  expect(document.body.textContent).toContain("нет объявленных событий");
  expect(button("Настроить условие").disabled).toBe(true);
  await act(async () => {
    const doc = structuredClone(editor.store.getState().document!);
    delete doc.nodes.n_heading!.locked;
    editor.store.setState({ document: doc });
    editor.locale("en");
  });
  await click("Configure condition");
  expect(document.body.textContent).toContain("Visibility condition");
  await act(async () =>
    document
      .querySelector("form")!
      .dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })),
  );
  expect(document.querySelector("form")).toBeNull();
});
