// @vitest-environment jsdom
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import type { PlannedAction, RenderPlan } from "@cms/runtime";
import { CmsPage, type CmsPageProps, type NativeProps } from "../src/page.js";

const roots: Root[] = [];
afterEach(() => {
  act(() => roots.splice(0).forEach((root) => root.unmount()));
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
function page(type: string, chain: PlannedAction[], options: Partial<CmsPageProps> = {}) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const plan: RenderPlan = {
    roots: [
      {
        id: "button",
        key: "repeat:one/button",
        type,
        props: { label: "Select", href: "/safe" },
        children: [],
        className: "",
        events: { click: chain },
      },
    ],
    css: "",
    diagnostics: [],
  };
  const host = document.createElement("div"),
    root = createRoot(host);
  document.body.append(host);
  roots.push(root);
  let props!: NativeProps;
  const original = options.registry?.[type];
  const registry = {
    ...options.registry,
    [type]: (value: NativeProps) => {
      props = value;
      return original ? createElement(original, value) : null;
    },
  };
  act(() => root.render(createElement(CmsPage, { plan, ...options, registry })));
  return { host, props };
}
it("IR-050/SDK-013: React передаёт native-события, сервисы и безопасную ошибку экземпляра", async () => {
  const handler = vi.fn(),
    app = { select: true },
    onActionError = vi.fn();
  const actions = [
    { name: "test.select", args: { id: { type: "string" as const, required: true } }, handler },
  ];
  const Component = vi.fn(() => null);
  const element = page("Native", [{ action: "test.select", args: { id: "one" } }], {
    registry: { Native: Component },
    actions,
    app,
    onActionError,
  });
  expect(Component).toHaveBeenCalledOnce();
  expect(await element.props.events!.click!()).toBe(true);
  expect(handler).toHaveBeenCalledWith(
    { id: "one" },
    expect.objectContaining({ app, node: "button", instance: "repeat:one/button" }),
  );
  handler.mockRejectedValueOnce(new Error("private-secret"));
  expect(await element.props.events!.click!()).toBe(false);
  expect(onActionError).toHaveBeenCalledWith({
    code: "CMS_ACTION_FAILED",
    action: "test.select",
    node: "button",
    instance: "repeat:one/button",
  });
  const track = vi.fn(),
    loadMore = vi.fn();
  const override = page(
    "Native",
    [
      { action: "track", args: { event: "view" } },
      { action: "data.loadMore", args: { source: "featured" } },
    ],
    {
      registry: { Native: Component },
      services: { track, loadMore },
    },
  );
  expect(await override.props.events!.click!()).toBe(true);
  expect(track).toHaveBeenCalledWith("view", {});
  expect(loadMore).toHaveBeenCalledWith("featured");
});
it("IR-050/SDK-033: Button и Link исполняют действия, режим дизайна блокирует их", async () => {
  const handler = vi.fn(),
    actions = [{ name: "test.select", args: {}, handler }];
  const chain = [{ action: "test.select", args: {} }];
  page("Button", chain, { actions }).host.querySelector<HTMLButtonElement>("button")!.click();
  expect(handler).toHaveBeenCalledTimes(1);
  const event = new MouseEvent("click", { bubbles: true, cancelable: true });
  page("Link", chain, { actions }).host.querySelector("a")!.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
  expect(handler).toHaveBeenCalledTimes(2);
  page("Button", chain, { actions, mode: "design" })
    .host.querySelector<HTMLButtonElement>("button")!
    .click();
  page("Link", chain, { actions, mode: "design" })
    .host.querySelector("a")!
    .dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  expect(handler).toHaveBeenCalledTimes(2);
  const plan: RenderPlan = {
    roots: [
      {
        id: "button",
        key: "button",
        type: "Native",
        props: {},
        children: [],
        className: "",
        events: { click: chain },
      },
    ],
    css: "",
    diagnostics: [],
    interactionMode: "design",
  };
  const native = page("Native", chain, { plan, actions });
  expect(await native.props.events!.click!()).toBe(false);
  expect(handler).toHaveBeenCalledTimes(2);
});
it("IR-052: встроенные React-сервисы используют адрес и DOM-цель текущего экземпляра", async () => {
  const dialog = document.createElement("dialog"),
    target = document.createElement("div");
  dialog.id = "modal";
  target.id = "target";
  dialog.showModal = vi.fn();
  dialog.close = vi.fn();
  target.scrollIntoView = vi.fn();
  document.body.append(dialog, target);
  const registry = { Native: () => null };
  async function run(action: string, args: Record<string, unknown>, onActionError = vi.fn()) {
    return page("Native", [{ action, args }], { registry, onActionError }).props.events!.click!();
  }
  expect(await run("navigate", { to: { kind: "url", url: "#safe" } })).toBe(true);
  expect(window.location.hash).toBe("#safe");
  expect(await run("scrollTo", { target: "target" })).toBe(true);
  expect(target.scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth" });
  expect(await run("openModal", { target: "modal" })).toBe(true);
  expect(dialog.showModal).toHaveBeenCalledOnce();
  expect(await run("closeModal", { target: "modal" })).toBe(true);
  expect(dialog.close).toHaveBeenCalledOnce();
  for (const action of ["scrollTo", "openModal", "closeModal"])
    expect(await run(action, { target: "missing" })).toBe(false);
  for (const action of ["openModal", "closeModal"])
    expect(await run(action, { target: "target" })).toBe(false);
  expect(
    await run("navigate", { to: { kind: "page", page: "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11" } }),
  ).toBe(false);
});
