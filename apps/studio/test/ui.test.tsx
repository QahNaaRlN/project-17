// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Studio } from "../src/ui.js";
import { createEditor, type Editor } from "../src/editor.js";
import { mockApi, documentId, changesetId } from "./fixture.js";
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
}
const input = (id: string) => document.getElementById(id) as HTMLInputElement;
const layer = (id: string) =>
  document.querySelector<HTMLElement>(`[role=treeitem][title="${id}"]`)!;
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>("button")].find(
    (item) => item.textContent === text,
  )!;
it("STU-001/003/010/021/026/033: открывает, выбирает, сохраняет текст/числа/токены, меняет локаль", async () => {
  const { api, session } = await setup();
  expect(document.body.textContent).toContain("Откройте страницу");
  const ids = document.querySelectorAll<HTMLInputElement>(".open-bar input");
  await change(ids[0]!, documentId);
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
  await change(input("design-padding"), "12");
  await submit(input("design-padding").closest("form")!);
  expect(session.record.body.nodes.n_heading!.design).toEqual({ padding: { raw: 12 } });
  const point = document.querySelector<HTMLSelectElement>('select[aria-label="Размер экрана"]')!;
  await change(point, "sm");
  await change(input("design-padding"), "16");
  await submit(input("design-padding").closest("form")!);
  expect(session.record.body.nodes.n_heading!.design).toEqual({
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
  await change(input("design-gap"), "-1");
  await submit(input("design-gap").closest("form")!);
  expect(editor.store.getState().error).toBe("FIELD_INVALID");
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
