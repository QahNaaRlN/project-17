// @vitest-environment jsdom
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import type { RenderPlan, RuntimeAction } from "@cms/runtime";
import { CmsPage, type NativeProps } from "../src/page.js";

let root: Root | undefined;
afterEach(async () => {
  await act(async () => root?.unmount());
  root = undefined;
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});

it.each(["plan", "mode", "unmount"])(
  "STU-248/249: a suspended chain cannot resume after %s changes, even when interactions are ready again",
  async (change) => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    let finish!: () => void;
    const waiting = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const record = vi.fn();
    const actions: RuntimeAction[] = [
      { name: "test.wait", args: {}, handler: waiting },
      { name: "test.record", args: { value: { type: "string", required: true } }, handler: record },
    ];
    const plan = (value: string): RenderPlan => ({
      roots: [
        {
          id: "button",
          key: "button",
          type: "Native",
          props: {},
          children: [],
          className: "",
          events: {
            click: [
              { action: "test.wait", args: {} },
              { action: "test.record", args: { value } },
            ],
          },
        },
      ],
      css: "",
      diagnostics: [],
    });
    let events!: NonNullable<NativeProps["events"]>;
    const registry = {
      Native: (props: NativeProps) => {
        events = props.events!;
        return null;
      },
    };
    const canInteract = () => true;
    const before = plan("before");
    const render = async (next: RenderPlan, mode: "design" | "interact" = "interact") => {
      await act(async () =>
        root!.render(createElement(CmsPage, { plan: next, mode, actions, registry, canInteract })),
      );
    };
    await render(before);
    const obsolete = events.click!;
    const pending = obsolete();
    expect(waiting).toHaveBeenCalledOnce();
    if (change === "plan") await render(plan("after"));
    else if (change === "mode") {
      await render(before, "design");
      await render(before, "interact");
    } else {
      await act(async () => root!.unmount());
      root = undefined;
    }
    finish();
    expect(await pending).toBe(false);
    expect(record).not.toHaveBeenCalled();
    expect(await obsolete()).toBe(false);
    expect(waiting).toHaveBeenCalledOnce();
    if (change !== "unmount") {
      waiting.mockResolvedValue(undefined);
      expect(await events.click!()).toBe(true);
      expect(record).toHaveBeenCalledWith(
        { value: change === "plan" ? "after" : "before" },
        expect.anything(),
      );
    }
  },
);
