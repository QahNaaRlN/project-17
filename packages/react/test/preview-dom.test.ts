import { JSDOM } from "jsdom";
import { afterEach, expect, it, vi } from "vitest";
import { documentHash, preparePage } from "@cms/runtime";
import type { IrDocument } from "@cms/ir";
import { attachPreviewBridge } from "../src/preview.js";
import { CmsPage, type NativeProps } from "../src/page.js";
import { createElement } from "react";
import { renderToString } from "react-dom/server";
const doc: IrDocument = {
  irVersion: "1.0",
  kind: "page",
  root: "rept",
  nodes: {
    rept: { id: "rept", type: "Repeat", props: { items: ["one", "two"] }, children: ["text"] },
    text: { id: "text", type: "Button", bindings: { label: "$item" } },
  },
};
let cleanup: (() => void) | undefined;
afterEach(() => {
  cleanup?.();
  cleanup = undefined;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
it("SDK-030: preview маркирует примитивы и native, published DOM не содержит метаданных", () => {
  const plan = preparePage(doc),
    published = renderToString(createElement(CmsPage, { plan }));
  expect(published).not.toContain("data-cms-");
  const html = renderToString(createElement(CmsPage, { plan: { ...plan, preview: true } }));
  expect(html.match(/data-cms-node="text"/g)).toHaveLength(2);
  expect(html).toContain('data-cms-instance="rept:number%3A0/text"');
  const Component = ({ previewAttributes }: NativeProps) =>
    createElement("article", previewAttributes);
  const nativePlan = {
    roots: [{ id: "text", key: "text", type: "Native", props: {}, className: "", children: [] }],
    css: "",
    diagnostics: [],
    preview: true,
  };
  expect(
    renderToString(createElement(CmsPage, { plan: nativePlan, registry: { Native: Component } })),
  ).toBe('<article data-cms-node="text"></article>');
});
it("SDK-031/033/042: design перехватывает ближайший экземпляр, interact разрешает действие, layout ограничен 60 мс", async () => {
  vi.useFakeTimers();
  const dom = new JSDOM(
    '<main id="root" data-cms-node="rept"><button data-cms-node="text" data-cms-instance="one"><span>select</span></button><button data-cms-node="text" data-cms-instance="two">two</button><i data-cms-node="foreign"></i></main>',
    { url: "https://app.example" },
  );
  const win = dom.window,
    root = win.document.getElementById("root")!;
  const send = vi.spyOn(win, "postMessage").mockImplementation(() => {}),
    action = vi.fn();
  root.querySelector("button")!.addEventListener("click", action);
  const bridge = await attachPreviewBridge({
    root,
    window: win as unknown as Window,
    studioOrigin: "https://studio.example",
    manifestHash: "sha256:" + "a".repeat(64),
    initial: { documentId: "doc", path: "/", document: doc, hash: await documentHash(doc) },
    reload: async () => {
      throw new Error("private");
    },
    render: async () => {},
  });
  cleanup = () => {
    bridge.dispose();
    dom.window.close();
  };
  await bridge.controller.receive({
    origin: "https://studio.example",
    source: win.parent,
    data: {
      cms: "preview",
      v: 1,
      id: "init",
      type: "init",
      payload: { sessionId: "session", locale: "ru", mode: "design" },
    },
  });
  const event = new win.MouseEvent("click", { bubbles: true, cancelable: true, shiftKey: true });
  root.querySelector("span")!.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
  expect(action).not.toHaveBeenCalled();
  const click = send.mock.calls.find(([value]) => value.type === "node.click")![0];
  expect(click.payload).toMatchObject({
    nodeId: "text",
    instance: "one",
    modifiers: { shift: true },
  });
  root.querySelector("span")!.dispatchEvent(new win.MouseEvent("mouseover", { bubbles: true }));
  root.dispatchEvent(new win.MouseEvent("mouseleave"));
  await vi.advanceTimersByTimeAsync(0);
  const layouts = () => send.mock.calls.filter(([message]) => message.type === "layout");
  expect(layouts()).toHaveLength(1);
  expect(layouts()[0]![0].payload.nodes.text).toHaveLength(2);
  expect(layouts()[0]![0].payload.nodes.foreign).toBeUndefined();
  win.dispatchEvent(new win.Event("resize"));
  win.dispatchEvent(new win.Event("scroll"));
  await vi.advanceTimersByTimeAsync(59);
  expect(layouts()).toHaveLength(1);
  await vi.advanceTimersByTimeAsync(1);
  expect(layouts()).toHaveLength(2);
  win.dispatchEvent(
    new win.MessageEvent("message", {
      origin: "https://studio.example",
      source: win.parent as unknown as Window,
      data: { cms: "preview", v: 1, id: "mode", type: "mode", payload: { mode: "interact" } },
    }),
  );
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
  root.querySelector("button")!.click();
  expect(action).toHaveBeenCalledOnce();
  const count = send.mock.calls.length;
  bridge.dispose();
  win.dispatchEvent(new win.Event("resize"));
  root.querySelector("button")!.click();
  await vi.advanceTimersByTimeAsync(60);
  expect(send).toHaveBeenCalledTimes(count);
  expect(action).toHaveBeenCalledTimes(2);
});
it("SDK-033/042: root без атрибута не создаёт выбор вне узлов, listeners удаляются без observer", async () => {
  vi.useFakeTimers();
  const dom = new JSDOM(
    '<main><a href="/next">plain</a><button data-cms-node="text">text</button></main>',
    { url: "https://app.example" },
  );
  const win = dom.window;
  Object.defineProperty(win, "MutationObserver", { value: undefined });
  const send = vi.spyOn(win, "postMessage").mockImplementation(() => {});
  const bridge = await attachPreviewBridge({
    root: win.document.querySelector("main")!,
    window: win as unknown as Window,
    studioOrigin: "https://studio.example",
    manifestHash: "sha256:" + "a".repeat(64),
    initial: { documentId: "doc", path: "/", document: doc, hash: await documentHash(doc) },
    reload: async () => {
      throw new Error();
    },
    render: async () => {},
  });
  cleanup = () => {
    bridge.dispose();
    win.close();
  };
  win.document.querySelector("a")!.click();
  expect(send.mock.calls.some(([value]) => value.type === "node.click")).toBe(false);
  win.document
    .querySelector("button")!
    .dispatchEvent(new win.MouseEvent("mouseover", { bubbles: true }));
  win.document
    .querySelector("button")!
    .dispatchEvent(new win.MouseEvent("mouseover", { bubbles: true }));
  expect(send.mock.calls.filter(([value]) => value.type === "node.hover")).toHaveLength(1);
  bridge.dispose();
  await vi.advanceTimersByTimeAsync(60);
});

it("STU-366: a placeholder renders a neutral preview block and nothing when published", () => {
  const pendingPlan = {
    roots: [
      {
        id: "n_promo",
        key: "n_promo",
        type: "PromoBanner",
        props: {},
        className: "",
        pending: "0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11",
        children: [{ id: "n_btn", key: "n_btn", type: "Button", props: { label: "Buy" }, className: "", children: [] }],
      },
    ],
    css: "",
    diagnostics: [],
  };
  const Real = () => createElement("section", null, "real");
  const preview = renderToString(
    createElement(CmsPage, { plan: { ...pendingPlan, preview: true }, registry: { PromoBanner: Real } }),
  );
  expect(preview).toContain('data-cms-pending="0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11"');
  expect(preview).toContain("PromoBanner");
  expect(preview).toContain("Buy");
  expect(preview).not.toContain("real");
  expect(renderToString(createElement(CmsPage, { plan: pendingPlan, registry: { PromoBanner: Real } }))).toBe("");
});
