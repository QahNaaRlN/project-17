import { Children, isValidElement, type ReactElement } from "react";
import { afterEach, expect, it, vi } from "vitest";
import type { PlannedAction, RenderPlan } from "@cms/runtime";
import { CmsPage, type CmsPageProps, type NativeProps } from "../src/page.js";

afterEach(() => vi.unstubAllGlobals());
function page(type: string, chain: PlannedAction[], options: Partial<CmsPageProps> = {}) {
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
  const fragment = CmsPage({ plan, ...options }) as ReactElement<{ children: unknown }>;
  return Children.toArray(fragment.props.children as never).find(
    (child) => isValidElement(child) && child.type !== "style",
  ) as ReactElement<NativeProps & { onClick: (event?: unknown) => void }>;
}
it("IR-050/SDK-013: React передаёт native-события, сервисы и безопасную ошибку экземпляра", async () => {
  const handler = vi.fn(),
    app = { select: true },
    onActionError = vi.fn();
  const actions = [
    { name: "test.select", args: { id: { type: "string" as const, required: true } }, handler },
  ];
  const Component = () => null;
  const element = page("Native", [{ action: "test.select", args: { id: "one" } }], {
    registry: { Native: Component },
    actions,
    app,
    onActionError,
  });
  expect(element.type).toBe(Component);
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
    { registry: { Native: Component }, services: { track, loadMore } },
  );
  expect(await override.props.events!.click!()).toBe(true);
  expect(track).toHaveBeenCalledWith("view", {});
  expect(loadMore).toHaveBeenCalledWith("featured");
});
it("IR-050/SDK-033: Button и Link исполняют действия, режим дизайна блокирует их", async () => {
  const handler = vi.fn();
  const actions = [{ name: "test.select", args: {}, handler }];
  const chain = [{ action: "test.select", args: {} }];
  page("Button", chain, { actions }).props.onClick();
  expect(handler).toHaveBeenCalledTimes(1);
  const event = { preventDefault: vi.fn() };
  page("Link", chain, { actions }).props.onClick(event);
  expect(event.preventDefault).toHaveBeenCalledOnce();
  expect(handler).toHaveBeenCalledTimes(2);
  page("Button", chain, { actions, mode: "design" }).props.onClick();
  page("Link", chain, { actions, mode: "design" }).props.onClick(event);
  expect(handler).toHaveBeenCalledTimes(2);
  const plan = {
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
    interactionMode: "design" as const,
  };
  const fragment = CmsPage({ plan, registry: { Native: () => null }, actions }) as ReactElement<{
    children: unknown;
  }>;
  const native = Children.toArray(fragment.props.children as never)[0] as ReactElement<NativeProps>;
  expect(await native.props.events!.click!()).toBe(false);
  expect(handler).toHaveBeenCalledTimes(2);
});
it("IR-052: встроенные React-сервисы используют адрес и DOM-цель текущего экземпляра", async () => {
  const assign = vi.fn();
  vi.stubGlobal("window", { location: { assign } });
  class Dialog {
    showModal = vi.fn();
    close = vi.fn();
  }
  const dialog = new Dialog(),
    scrollIntoView = vi.fn();
  const getElementById = vi.fn((target: string) =>
    target === "modal" ? dialog : target === "target" ? { scrollIntoView } : null,
  );
  vi.stubGlobal("HTMLDialogElement", Dialog);
  vi.stubGlobal("document", { getElementById });
  const registry = { Native: () => null };
  async function run(action: string, args: Record<string, unknown>, onActionError = vi.fn()) {
    return page("Native", [{ action, args }], { registry, onActionError }).props.events!.click!();
  }
  expect(await run("navigate", { to: { kind: "url", url: "/safe" } })).toBe(true);
  expect(assign).toHaveBeenCalledWith("/safe");
  expect(await run("scrollTo", { target: "target" })).toBe(true);
  expect(scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth" });
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
