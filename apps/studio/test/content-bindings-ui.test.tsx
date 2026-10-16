// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ContentBindingsPanel } from "../src/content-bindings-panel.js";
import { createEditor, type Editor } from "../src/editor.js";
import { fixture, mockApi, documentId, changesetId } from "./fixture.js";
let root: Root, editor: Editor, client: QueryClient;
afterEach(async () => {
  await act(async () => root?.unmount());
  editor?.dispose();
  client?.clear();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const button = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === label)!;
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}
async function click(label: string) {
  await act(async () => button(label).click());
  await flush();
}
async function change(label: string, value: string) {
  const input = [...document.querySelectorAll("label")]
    .find((l) => l.textContent?.startsWith(label))!
    .querySelector("input,select") as HTMLInputElement | HTMLSelectElement;
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(
      input.tagName === "SELECT" ? HTMLSelectElement.prototype : HTMLInputElement.prototype,
      "value",
    )!.set!;
    setter.call(input, value);
    input.dispatchEvent(new Event("change", { bubbles: true }));
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await flush();
}
async function setup(rights?: string[], initial = false) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const source = fixture();
  source.manifest.schemas = {
    Article: {
      version: 1,
      fields: {
        title: { type: "text", localized: true },
        image: { type: "asset", assetKind: "image" },
        count: { type: "number" },
      },
    },
  };
  if (initial)
    source.record.body.content = {
      schema: "Article",
      resolve: { by: "entity", entityId: documentId },
    };
  const { api, session } = mockApi(source);
  vi.mocked(api.contentCatalog).mockResolvedValue({
    items: [{ id: documentId, kind: "entity", schema: "Article", title: "Story", deleted: false }],
    nextCursor: null,
    locales: ["ru", "en"],
    defaultLocale: "ru",
  });
  vi.mocked(api.content).mockResolvedValue({
    id: documentId,
    kind: "entity",
    schema: "Article",
    schemaVersion: 1,
    versionId: documentId,
    deleted: false,
    data: { title: { ru: "История", en: "Story" } },
  });
  editor = createEditor(api);
  await editor.open(documentId, changesetId);
  editor.select("n_heading");
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  document.body.innerHTML = "<div id='test-root'></div>";
  root = createRoot(document.getElementById("test-root")!);
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <ContentBindingsPanel {...{ api, editor, rights }} />
      </QueryClientProvider>,
    ),
  );
  await flush();
  return { api, session };
}
it("STU-203/204/206/208/216: выбор сущности и поля подтверждается явно, Escape возвращает фокус", async () => {
  const { api } = await setup();
  await click("Выбрать сущность");
  expect(document.querySelector('[role="dialog"]')).not.toBeNull();
  await change("Схема", "Article");
  await change("Поиск сущности", "Story");
  await vi.waitFor(() =>
    expect(document.querySelector(`option[value="${documentId}"]`)).not.toBeNull(),
  );
  await change("Сущность", documentId);
  await vi.waitFor(() => expect(button("Подтвердить").disabled).toBe(false));
  await click("Подтвердить");
  expect(editor.store.getState().document!.content?.schema).toBe("Article");
  expect(api.apply).toHaveBeenCalled();
  await click("Связать с контентом · text");
  await change("Поле контента", "$content.title");
  expect(document.body.textContent).toContain("История");
  await click("Подтвердить");
  expect(editor.store.getState().document!.nodes.n_heading!.bindings!.text).toBe("$content.title");
  await click("Связать с контентом · text");
  await act(async () =>
    document
      .querySelector('[role="dialog"]')!
      .dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })),
  );
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(document.activeElement).toBe(button("Связать с контентом · text"));
  expect(editor.store.getState().document!.nodes.n_heading!.bindings!.text).toBe("$content.title");
});
it("STU-209/216: удаление источника перечисляет снимаемые связи, read-only не читает каталог", async () => {
  const { api } = await setup(["design.read"]);
  expect(document.body.textContent).toContain("нужно право чтения контента");
  expect(api.contentCatalog).not.toHaveBeenCalled();
});
it("STU-209/216: источник и его связи удаляются одной группой без копирования значения", async () => {
  const { api } = await setup(undefined, true);
  await click("Выбрать сущность");
  const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
  await act(async () => checkbox.click());
  await flush();
  expect(document.body.textContent).toContain("n_footer");
  await click("Подтвердить");
  expect(editor.store.getState().document).not.toHaveProperty("content");
  const ops = vi.mocked(api.apply).mock.calls.at(-1)![1];
  expect(ops.map((op) => op.type)).toEqual(["node.setBinding", "document.setContent"]);
});
